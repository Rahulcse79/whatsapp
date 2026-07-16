package devices

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// ErrNotFound is returned by the repositories when no row matches.
var ErrNotFound = errors.New("devices: not found")

// Repo is the device-registry persistence port.
type Repo interface {
	ListByUser(ctx context.Context, userID string) ([]Device, error)
	Get(ctx context.Context, deviceID string) (Device, error)
	Rename(ctx context.Context, userID, deviceID, name string) (bool, error)
	CountActive(ctx context.Context, userID string) (int, error)
	// RevokeDevice atomically revokes the device and tears down everything
	// bound to it (sessions, prekeys, push token). Returns false if the
	// device isn't the user's or is already revoked.
	RevokeDevice(ctx context.Context, userID, deviceID string) (bool, error)
}

// LinkRepo persists the QR link handoff.
type LinkRepo interface {
	CreateLink(ctx context.Context, l Link) error
	GetLink(ctx context.Context, token string) (Link, error)
	// ApproveLink registers the new linked device and marks the link
	// approved in one transaction.
	ApproveLink(ctx context.Context, p ApproveParams) error
	ConsumeLink(ctx context.Context, token string) error
}

// ApproveParams is the approve transaction input.
type ApproveParams struct {
	Token       string
	UserID      string
	NewDeviceID string
	ApprovedBy  string
	Platform    int16
	Name        string
	IdentityKey []byte
	Cert        []byte
	Now         time.Time
}

// SessionMinter creates a session + tokens for an already-registered device.
// Satisfied by *auth.Service (token issuance stays in the auth context).
type SessionMinter interface {
	MintLinkedSession(ctx context.Context, userID, deviceID string) (access, refresh, sessionID string, err error)
}

// Events emits device lifecycle facts (consumed by the session killer and
// the user's other devices). Noop until the events infra is wired.
type Events interface {
	DeviceAdded(ctx context.Context, userID, deviceID string)
	DeviceRevoked(ctx context.Context, userID, deviceID string)
}

// Service orchestrates device management and linking.
type Service struct {
	repo    Repo
	links   LinkRepo
	minter  SessionMinter
	events  Events
	now     func() time.Time
	entropy io.Reader
}

func NewService(repo Repo, links LinkRepo, minter SessionMinter, events Events) *Service {
	return &Service{repo: repo, links: links, minter: minter, events: events,
		now: time.Now, entropy: rand.Reader}
}

// List returns the caller's devices (GET /v1/devices).
func (s *Service) List(ctx context.Context, ident auth.Identity) ([]View, error) {
	devs, err := s.repo.ListByUser(ctx, ident.UserID)
	if err != nil {
		return nil, httpx.Transient()
	}
	views := make([]View, 0, len(devs))
	for _, d := range devs {
		v := View{ID: d.ID, IsPrimary: d.IsPrimary, Platform: platformName(d.Platform), Name: d.Name}
		if !d.LastActiveAt.IsZero() {
			v.LastActive = d.LastActiveAt.UnixMilli()
		}
		views = append(views, v)
	}
	return views, nil
}

// Rename sets a device's display name (PATCH /v1/devices/{id}).
func (s *Service) Rename(ctx context.Context, ident auth.Identity, deviceID, name string) error {
	if name == "" || len(name) > 100 {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_NAME", "name must be 1–100 characters")
	}
	ok, err := s.repo.Rename(ctx, ident.UserID, deviceID, name)
	if err != nil {
		return httpx.Transient()
	}
	if !ok {
		return httpx.Reject(http.StatusNotFound, "DEVICE_NOT_FOUND", "no such device")
	}
	return nil
}

// Revoke atomically tears down a device (DELETE /v1/devices/{id}).
func (s *Service) Revoke(ctx context.Context, ident auth.Identity, deviceID string) error {
	ok, err := s.repo.RevokeDevice(ctx, ident.UserID, deviceID)
	if err != nil {
		return httpx.Transient()
	}
	if !ok {
		return httpx.Reject(http.StatusNotFound, "DEVICE_NOT_FOUND", "no such active device")
	}
	s.events.DeviceRevoked(ctx, ident.UserID, deviceID)
	return nil
}

// ── QR linking ───────────────────────────────────────────────────────────

// LinkInitRequest is what a new (unauthenticated) device submits.
type LinkInitRequest struct {
	Platform    string
	Name        string
	IdentityKey []byte
}

// LinkInitResult carries the token the device shows in its QR and polls with.
type LinkInitResult struct {
	LinkToken string `json:"link_token"`
	QRPayload string `json:"qr_payload"`
}

// LinkInit creates a pending link (POST /v1/devices/link/init).
func (s *Service) LinkInit(ctx context.Context, req LinkInitRequest) (LinkInitResult, error) {
	platform, ok := platformCode(req.Platform)
	if !ok || len(req.IdentityKey) == 0 {
		return LinkInitResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_DEVICE",
			"platform (ios|android|web) and identity_key are required")
	}
	tokenRaw := make([]byte, 32)
	if _, err := io.ReadFull(s.entropy, tokenRaw); err != nil {
		return LinkInitResult{}, httpx.Transient()
	}
	token := base64.RawURLEncoding.EncodeToString(tokenRaw)
	now := s.now()
	link := Link{
		Token: token, Platform: platform, Name: req.Name, IdentityKey: req.IdentityKey,
		State: LinkPending, CreatedAt: now, ExpiresAt: now.Add(LinkTTL),
	}
	if err := s.links.CreateLink(ctx, link); err != nil {
		return LinkInitResult{}, httpx.Transient()
	}
	// The primary scans this: it needs the token (to approve) and the new
	// device's identity key (to sign it).
	qr, _ := json.Marshal(map[string]string{
		"link_token":   token,
		"identity_key": base64.StdEncoding.EncodeToString(req.IdentityKey),
	})
	return LinkInitResult{LinkToken: token, QRPayload: string(qr)}, nil
}

// LinkApprove is the primary device approving a scanned link
// (POST /v1/devices/link/approve). cert is the primary's signature over the
// new device's identity key.
func (s *Service) LinkApprove(ctx context.Context, ident auth.Identity, token string, cert []byte) error {
	if len(cert) == 0 {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_CERT",
			"the primary must sign the new device (cert required)")
	}
	link, err := s.links.GetLink(ctx, token)
	if errors.Is(err, ErrNotFound) {
		return httpx.Reject(http.StatusNotFound, "LINK_NOT_FOUND", "unknown or expired link")
	}
	if err != nil {
		return httpx.Transient()
	}
	if s.now().After(link.ExpiresAt) {
		return httpx.Reject(http.StatusGone, "LINK_EXPIRED", "link expired; start over")
	}
	if link.State != LinkPending {
		return httpx.Reject(http.StatusConflict, "LINK_ALREADY_HANDLED", "link already approved")
	}

	approver, err := s.repo.Get(ctx, ident.DeviceID)
	if err != nil {
		return httpx.Transient()
	}
	if !approver.IsPrimary {
		return httpx.Reject(http.StatusForbidden, "NOT_PRIMARY_DEVICE",
			"only the primary device may approve linking")
	}

	active, err := s.repo.CountActive(ctx, ident.UserID)
	if err != nil {
		return httpx.Transient()
	}
	if active >= MaxDevicesPerUser {
		return httpx.Reject(http.StatusForbidden, "STATE_DEVICE_LIMIT",
			"device limit reached; revoke a device first")
	}

	newDeviceID := id.New()
	err = s.links.ApproveLink(ctx, ApproveParams{
		Token: token, UserID: ident.UserID, NewDeviceID: newDeviceID,
		ApprovedBy: ident.DeviceID, Platform: link.Platform, Name: link.Name,
		IdentityKey: link.IdentityKey, Cert: cert, Now: s.now(),
	})
	if errors.Is(err, ErrNotFound) {
		return httpx.Reject(http.StatusConflict, "LINK_ALREADY_HANDLED", "link already approved")
	}
	if err != nil {
		return httpx.Transient()
	}
	s.events.DeviceAdded(ctx, ident.UserID, newDeviceID)
	return nil
}

// LinkResult is returned to the polling new device.
type LinkResult struct {
	Pending      bool   `json:"pending,omitempty"`
	AccessJWT    string `json:"access_jwt,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	DeviceID     string `json:"device_id,omitempty"`
}

// LinkComplete is polled by the new device (POST /v1/devices/link/complete).
// While pending it returns {pending:true}; once approved it mints the
// device's session and returns tokens, then consumes the link.
func (s *Service) LinkComplete(ctx context.Context, token string) (LinkResult, error) {
	link, err := s.links.GetLink(ctx, token)
	if errors.Is(err, ErrNotFound) {
		return LinkResult{}, httpx.Reject(http.StatusNotFound, "LINK_NOT_FOUND", "unknown or expired link")
	}
	if err != nil {
		return LinkResult{}, httpx.Transient()
	}
	if s.now().After(link.ExpiresAt) {
		return LinkResult{}, httpx.Reject(http.StatusGone, "LINK_EXPIRED", "link expired; start over")
	}
	switch link.State {
	case LinkPending:
		return LinkResult{Pending: true}, nil
	case LinkConsumed:
		return LinkResult{}, httpx.Reject(http.StatusConflict, "LINK_ALREADY_USED", "link already completed")
	case LinkApproved:
		// proceed
	}

	access, refresh, _, err := s.minter.MintLinkedSession(ctx, link.UserID, link.DeviceID)
	if err != nil {
		return LinkResult{}, httpx.Transient()
	}
	if err := s.links.ConsumeLink(ctx, token); err != nil {
		return LinkResult{}, httpx.Transient()
	}
	return LinkResult{AccessJWT: access, RefreshToken: refresh, DeviceID: link.DeviceID}, nil
}
