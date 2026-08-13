package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/whatsapp-v2/server/internal/admin/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Sentinel wire errors. Authentication vs authorization are distinct: a bad
// token is 401 (re-auth); a valid admin whose role is too low is 403.
var (
	errUnauthorized = httpx.Reject(http.StatusUnauthorized, "ADMIN_UNAUTHENTICATED", "invalid or expired admin token")
	errForbidden    = httpx.Reject(http.StatusForbidden, "ADMIN_FORBIDDEN", "insufficient role for this action")
	errNotFound     = httpx.Reject(http.StatusNotFound, "ADMIN_NOT_FOUND", "not found")
	errReason       = httpx.Reject(http.StatusBadRequest, "ADMIN_REASON_REQUIRED", "a reason is required for this action")
	errResolution   = httpx.Reject(http.StatusBadRequest, "ADMIN_BAD_RESOLUTION", "resolution must be dismiss, warn, or suspend")
)

// ErrNotFound lets adapters signal a missing row without importing httpx.
var ErrNotFound = errors.New("admin: not found")

const (
	defaultLimit = 50
	maxLimit     = 200
)

// Service is the admin plane. It authenticates admins via OIDC, gates every
// method on the RBAC lattice, and guarantees that no mutation lands without an
// audit row (the mutating store methods write both in one transaction).
type Service struct {
	verify  Verifier
	reports Reports
	users   Users
	audit   Audit
}

func NewService(v Verifier, reports Reports, users Users, audit Audit) *Service {
	return &Service{verify: v, reports: reports, users: users, audit: audit}
}

// Authenticate verifies an OIDC ID token and resolves the admin's role. A token
// that fails verification is unauthenticated; a token whose role claim is not a
// known admin role is forbidden (a valid IdP user who is not an admin).
func (s *Service) Authenticate(ctx context.Context, token string) (Identity, error) {
	claims, err := s.verify.Verify(ctx, token)
	if err != nil {
		return Identity{}, errUnauthorized
	}
	role, ok := domain.ParseRole(strings.TrimSpace(claims.Role))
	if !ok {
		return Identity{}, errForbidden
	}
	return Identity{Subject: claims.Subject, Email: claims.Email, Role: role}, nil
}

func require(admin Identity, min domain.Role) error {
	if !admin.Role.AtLeast(min) {
		return errForbidden
	}
	return nil
}

func clampLimit(n int) int {
	if n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

// ListReports returns the open report queue, newest-actionable first. Viewer+.
func (s *Service) ListReports(ctx context.Context, admin Identity, limit int) ([]Report, error) {
	if err := require(admin, domain.RoleViewer); err != nil {
		return nil, err
	}
	out, err := s.reports.ListOpen(ctx, clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	return out, nil
}

// GetReport returns one report's metadata. Viewer+.
func (s *Service) GetReport(ctx context.Context, admin Identity, id string) (Report, error) {
	if err := require(admin, domain.RoleViewer); err != nil {
		return Report{}, err
	}
	rep, err := s.reports.Get(ctx, id)
	if err != nil {
		return Report{}, mapNotFound(err)
	}
	return rep, nil
}

// ResolveReport closes a report. Dismiss/warn are agent-level; suspend is
// operator-level and additionally flips the target user's status. The state
// change, the (optional) suspension, and the audit row are one transaction.
func (s *Service) ResolveReport(ctx context.Context, admin Identity, id string, res domain.Resolution, reason string) error {
	if !res.Valid() {
		return errResolution
	}
	if err := require(admin, res.MinRole()); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return errReason
	}
	// The target is needed only for a suspension; read the report first.
	rep, err := s.reports.Get(ctx, id)
	if err != nil {
		return mapNotFound(err)
	}
	if rep.State != domain.ReportOpen {
		return errNotFound // already resolved — nothing open to action
	}
	suspend := ""
	if res == domain.Suspend {
		suspend = rep.TargetUserID
	}
	entry := AuditEntry{Actor: admin.Subject, Action: "report." + string(res), Target: id, Reason: reason}
	if err := s.reports.Resolve(ctx, id, res.FinalState(), suspend, entry); err != nil {
		return mapNotFound(err)
	}
	return nil
}

// SearchUsers finds users by username or phone-hash for T&S lookup. Agent+.
func (s *Service) SearchUsers(ctx context.Context, admin Identity, query string, limit int) ([]UserSummary, error) {
	if err := require(admin, domain.RoleAgent); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return nil, httpx.Reject(http.StatusBadRequest, "ADMIN_EMPTY_QUERY", "query must not be empty")
	}
	out, err := s.users.Search(ctx, strings.TrimSpace(query), clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	return out, nil
}

// UserMetadata returns a user's metadata summary (no content). Agent+.
func (s *Service) UserMetadata(ctx context.Context, admin Identity, userID string) (UserSummary, error) {
	if err := require(admin, domain.RoleAgent); err != nil {
		return UserSummary{}, err
	}
	sum, err := s.users.Summary(ctx, userID)
	if err != nil {
		return UserSummary{}, mapNotFound(err)
	}
	return sum, nil
}

// SuspendUser directly suspends a user (status → 1). Operator+. Audited.
func (s *Service) SuspendUser(ctx context.Context, admin Identity, userID, reason string) error {
	return s.setStatus(ctx, admin, userID, 1, "user.suspend", reason)
}

// ReactivateUser lifts a suspension (status → 0). Operator+. Audited.
func (s *Service) ReactivateUser(ctx context.Context, admin Identity, userID, reason string) error {
	return s.setStatus(ctx, admin, userID, 0, "user.reactivate", reason)
}

func (s *Service) setStatus(ctx context.Context, admin Identity, userID string, status int16, action, reason string) error {
	if err := require(admin, domain.RoleOperator); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return errReason
	}
	entry := AuditEntry{Actor: admin.Subject, Action: action, Target: userID, Reason: reason}
	if err := s.users.SetStatus(ctx, userID, status, entry); err != nil {
		return mapNotFound(err)
	}
	return nil
}

// ListAudit returns the append-only audit trail for review. Owner only — the
// audit log is the check on admins themselves, so only the top role reads it.
func (s *Service) ListAudit(ctx context.Context, admin Identity, limit int) ([]AuditRecord, error) {
	if err := require(admin, domain.RoleOwner); err != nil {
		return nil, err
	}
	out, err := s.audit.List(ctx, clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	return out, nil
}

// mapNotFound turns the store's not-found sentinel into a 404 and anything else
// into a retryable transient, so store internals never cross the edge.
func mapNotFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return errNotFound
	}
	return httpx.Transient()
}
