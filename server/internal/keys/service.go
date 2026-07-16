// Package keys distributes Signal prekey bundles (public material only) and
// consumes one-time prekeys atomically on session setup.
// Design: Docs/06-security/e2ee-design.md §1–2, Docs/04-api/auth-users-api.md.
package keys

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
)

// ErrNoDevices is returned when a bundle is requested for a user with no
// active devices (deleted account, or never registered).
var ErrNoDevices = errors.New("keys: user has no active devices")

// Repo is the persistence port.
type Repo interface {
	// ReplaceSignedPrekey upserts the device's current signed prekey.
	ReplaceSignedPrekey(ctx context.Context, deviceID string, sp SignedPrekey) error
	// AddOneTimePrekeys appends one-time prekeys, ignoring key_id collisions
	// (idempotent re-uploads).
	AddOneTimePrekeys(ctx context.Context, deviceID string, otps []OneTimePrekey) error
	// CountAvailable returns the device's unconsumed one-time prekey count.
	CountAvailable(ctx context.Context, deviceID string) (int, error)
	// ConsumeBundle returns one bundle per ACTIVE device of userID, each with
	// exactly one one-time prekey consumed atomically (or nil if exhausted).
	// Concurrent calls MUST never hand out the same one-time prekey twice.
	ConsumeBundle(ctx context.Context, userID string) ([]DeviceBundle, error)
}

// Limiter matches ratelimit.ValkeyLimiter; failures fail closed.
type Limiter interface {
	Allow(ctx context.Context, key string, p ratelimit.Params) (ratelimit.Result, error)
}

// Service orchestrates prekey publish and bundle fetch.
type Service struct {
	repo    Repo
	limiter Limiter
}

func NewService(repo Repo, limiter Limiter) *Service {
	return &Service{repo: repo, limiter: limiter}
}

// PublishResult tells the client whether it should upload more prekeys.
type PublishResult struct {
	Available int  `json:"available"`
	LowWater  bool `json:"low_water"`
}

// Publish stores a device's signed prekey and one-time prekeys (FR-AUTH,
// PUT /v1/keys/prekeys). deviceID comes from the authenticated identity —
// a device may only publish its own keys.
func (s *Service) Publish(ctx context.Context, deviceID string, sp SignedPrekey, otps []OneTimePrekey) (PublishResult, error) {
	if len(sp.Pubkey) == 0 || len(sp.Signature) == 0 {
		return PublishResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_PREKEYS",
			"signed_prekey pubkey and signature are required")
	}
	if len(otps) > MaxOneTimePrekeysPerUpload {
		return PublishResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_PREKEYS",
			fmt.Sprintf("at most %d one-time prekeys per upload", MaxOneTimePrekeysPerUpload))
	}
	for _, o := range otps {
		if len(o.Pubkey) == 0 {
			return PublishResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_PREKEYS",
				"every one-time prekey needs a pubkey")
		}
	}

	if err := s.repo.ReplaceSignedPrekey(ctx, deviceID, sp); err != nil {
		return PublishResult{}, httpx.Transient()
	}
	if len(otps) > 0 {
		if err := s.repo.AddOneTimePrekeys(ctx, deviceID, otps); err != nil {
			return PublishResult{}, httpx.Transient()
		}
	}
	avail, err := s.repo.CountAvailable(ctx, deviceID)
	if err != nil {
		return PublishResult{}, httpx.Transient()
	}
	return PublishResult{Available: avail, LowWater: avail < LowWaterMark}, nil
}

// FetchBundle returns bundles for every active device of targetUserID,
// consuming one one-time prekey per device. Rate-limited per requester as an
// enumeration defense (threat-model T11 neighbourhood).
func (s *Service) FetchBundle(ctx context.Context, requesterID, targetUserID string) ([]DeviceBundle, error) {
	res, err := s.limiter.Allow(ctx, "rl:keys:bundle:"+requesterID,
		ratelimit.Params{Rate: 10, Burst: 30})
	if err != nil {
		return nil, httpx.Transient()
	}
	if !res.Allowed {
		return nil, &httpx.APIError{Code: "RATE_LIMITED", Status: http.StatusTooManyRequests,
			Message: "too many bundle requests", Retryable: true, RetryAfter: res.RetryAfter}
	}

	bundles, err := s.repo.ConsumeBundle(ctx, targetUserID)
	if errors.Is(err, ErrNoDevices) {
		return nil, httpx.Reject(http.StatusNotFound, "USER_NOT_FOUND", "no active devices for user")
	}
	if err != nil {
		return nil, httpx.Transient()
	}
	if len(bundles) == 0 {
		return nil, httpx.Reject(http.StatusNotFound, "USER_NOT_FOUND", "no active devices for user")
	}
	return bundles, nil
}
