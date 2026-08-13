package flags

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// CacheTTL is how long an evaluated rule is cached (core-api-lld §5: 30 s). A
// flag change is visible on the writing pod immediately (the writer busts the
// key) and on every other pod within this window.
const CacheTTL = 30 * time.Second

// Named is a flag plus its stored rule and last-write metadata (management view).
type Named struct {
	Flag      string    `json:"flag"`
	Rule      Rule      `json:"rule"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store is the source of truth for rules (feature_flags). Get's second return
// distinguishes "flag absent" (present=false) from "flag present but disabled".
type Store interface {
	Get(ctx context.Context, flag string) (rule Rule, present bool, err error)
	List(ctx context.Context) ([]Named, error)
}

// Entry is one cached evaluation: the rule and whether the flag existed.
// Absent flags are cached too (negative caching) so a missing flag — the common
// case for a kill-switch that was never engaged — doesn't hit the store each time.
type Entry struct {
	Rule    Rule `json:"rule"`
	Present bool `json:"present"`
}

// Cache is the 30 s shared cache in front of the Store (Valkey in production).
type Cache interface {
	Get(ctx context.Context, flag string) (Entry, bool, error)
	Put(ctx context.Context, flag string, e Entry) error
	Del(ctx context.Context, flag string) error // bust on write
}

// Service evaluates flags through the cache. It is a platform primitive: any
// bounded context may depend on it; it depends on none.
type Service struct {
	store Store
	cache Cache
}

func NewService(store Store, cache Cache) *Service {
	return &Service{store: store, cache: cache}
}

// Enabled reports whether flag is on for subject. It fails CLOSED: if the rule
// can't be resolved (store error), the flag is treated as off — a feature gate
// we can't confirm stays dark.
func (s *Service) Enabled(ctx context.Context, flag, subject string) bool {
	rule, present := s.resolve(ctx, flag)
	if !present {
		return false
	}
	return rule.Eval(flag, subject)
}

// resolve returns the rule for flag and whether it exists, reading through the
// cache. A store error resolves to "absent" so Enabled fails closed.
func (s *Service) resolve(ctx context.Context, flag string) (Rule, bool) {
	if e, ok, err := s.cache.Get(ctx, flag); err == nil && ok {
		return e.Rule, e.Present
	}
	rule, present, err := s.store.Get(ctx, flag)
	if err != nil {
		return Rule{}, false
	}
	_ = s.cache.Put(ctx, flag, Entry{Rule: rule, Present: present})
	return rule, present
}

// KillSwitch is a named operational circuit breaker (core-api-lld §5).
type KillSwitch string

const (
	KillMediaUploads  KillSwitch = "kill.media_uploads"
	KillRegistrations KillSwitch = "kill.registrations"
	KillGroupCreation KillSwitch = "kill.group_creation"
	KillCalls         KillSwitch = "kill.calls"
)

// Allowed reports whether a feature guarded by a kill-switch may proceed. It is
// the inverse of Enabled(kill flag): the switch is a circuit breaker that is
// CLOSED (feature allowed) until an operator engages it. Because Enabled fails
// closed, Allowed fails OPEN — a flag-store outage never pauses the product.
func (s *Service) Allowed(ctx context.Context, sw KillSwitch, subject string) bool {
	return !s.Enabled(ctx, string(sw), subject)
}

// Guard binds an HTTP method + path prefix to a kill-switch.
type Guard struct {
	Method string
	Prefix string
	Switch KillSwitch
}

// CoreAPIGuards are the kill-switches enforceable at core-api's edge: pausing
// new registrations, group creation, and call setup. Media uploads live in
// media-svc, so KillMediaUploads is enforced there with the same middleware.
func CoreAPIGuards() []Guard {
	return []Guard{
		{http.MethodPost, "/v1/auth/request-otp", KillRegistrations},
		{http.MethodPost, "/v1/groups", KillGroupCreation},
		{http.MethodPost, "/v1/calls", KillCalls},
	}
}

// KillSwitchMiddleware pauses matching requests with 503 when their kill-switch
// is engaged — an operational circuit breaker at the routing edge, so incident
// response never needs a code deploy. Non-matching requests pass straight
// through. The subject is empty here (a global pause); partial rollouts on a
// kill-switch still work because Eval rides the rollout edges.
func (s *Service) KillSwitchMiddleware(guards []Guard) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, g := range guards {
				if r.Method == g.Method && strings.HasPrefix(r.URL.Path, g.Prefix) {
					if !s.Allowed(r.Context(), g.Switch, "") {
						httpx.Error(w, r, http.StatusServiceUnavailable, httpx.ErrorObj{
							Code:         "FEATURE_PAUSED",
							Message:      "this feature is temporarily paused",
							Retryable:    true,
							RetryAfterMS: CacheTTL.Milliseconds(),
						})
						return
					}
					break // one guard per route
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
