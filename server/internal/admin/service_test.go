package admin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/admin/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeVerifier struct {
	claims Claims
	err    error
}

func (f fakeVerifier) Verify(context.Context, string) (Claims, error) {
	return f.claims, f.err
}

// fakeStore models the transactional store: Resolve/SetStatus mutate state AND
// append the audit row together, so a test can assert both landed (or neither).
type fakeStore struct {
	reports map[string]Report
	users   map[string]*UserSummary
	audit   []AuditRecord
}

func newStore() *fakeStore {
	return &fakeStore{reports: map[string]Report{}, users: map[string]*UserSummary{}}
}

func (s *fakeStore) ListOpen(_ context.Context, limit int) ([]Report, error) {
	var out []Report
	for _, r := range s.reports {
		if r.State == domain.ReportOpen {
			out = append(out, r)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *fakeStore) Get(_ context.Context, id string) (Report, error) {
	r, ok := s.reports[id]
	if !ok {
		return Report{}, ErrNotFound
	}
	return r, nil
}

func (s *fakeStore) Resolve(_ context.Context, id string, state domain.ReportState, suspendUser string, audit AuditEntry) error {
	r, ok := s.reports[id]
	if !ok || r.State != domain.ReportOpen {
		return ErrNotFound
	}
	r.State = state
	s.reports[id] = r
	if suspendUser != "" {
		if u := s.users[suspendUser]; u != nil {
			u.Status = 1
		}
	}
	s.append(audit)
	return nil
}

func (s *fakeStore) Search(_ context.Context, query string, _ int) ([]UserSummary, error) {
	var out []UserSummary
	for _, u := range s.users {
		if strings.Contains(u.Username, query) {
			out = append(out, *u)
		}
	}
	return out, nil
}

func (s *fakeStore) Summary(_ context.Context, userID string) (UserSummary, error) {
	u, ok := s.users[userID]
	if !ok {
		return UserSummary{}, ErrNotFound
	}
	return *u, nil
}

func (s *fakeStore) SetStatus(_ context.Context, userID string, status int16, audit AuditEntry) error {
	u, ok := s.users[userID]
	if !ok {
		return ErrNotFound
	}
	u.Status = status
	s.append(audit)
	return nil
}

func (s *fakeStore) List(_ context.Context, _ int) ([]AuditRecord, error) { return s.audit, nil }

func (s *fakeStore) append(e AuditEntry) {
	s.audit = append(s.audit, AuditRecord{
		ID: int64(len(s.audit) + 1), Actor: e.Actor, Action: e.Action,
		Target: e.Target, Reason: e.Reason, At: time.Now(),
	})
}

// ── helpers ──────────────────────────────────────────────────────────────

func newSvc(store *fakeStore) *Service {
	return NewService(fakeVerifier{}, store, store, store)
}

func who(role domain.Role) Identity {
	return Identity{Subject: "admin-" + role.String(), Email: role.String() + "@x", Role: role}
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("not an APIError: %v", err)
	}
	return ae.Status
}

// ── Authenticate ─────────────────────────────────────────────────────────

func TestAuthenticate(t *testing.T) {
	t.Run("bad token → 401", func(t *testing.T) {
		s := NewService(fakeVerifier{err: errors.New("bad sig")}, nil, nil, nil)
		if _, err := s.Authenticate(context.Background(), "tok"); statusOf(t, err) != 401 {
			t.Fatalf("want 401, got %v", err)
		}
	})
	t.Run("valid IdP user but not an admin role → 403", func(t *testing.T) {
		s := NewService(fakeVerifier{claims: Claims{Subject: "u", Role: "engineering"}}, nil, nil, nil)
		if _, err := s.Authenticate(context.Background(), "tok"); statusOf(t, err) != 403 {
			t.Fatalf("want 403, got %v", err)
		}
	})
	t.Run("admin → identity with parsed role", func(t *testing.T) {
		s := NewService(fakeVerifier{claims: Claims{Subject: "u1", Email: "u1@x", Role: "operator"}}, nil, nil, nil)
		id, err := s.Authenticate(context.Background(), "tok")
		if err != nil || id.Role != domain.RoleOperator || id.Subject != "u1" {
			t.Fatalf("id=%+v err=%v", id, err)
		}
	})
}

// ── RBAC gating ──────────────────────────────────────────────────────────

func TestRBACGating(t *testing.T) {
	ctx := context.Background()
	store := newStore()
	store.reports["r1"] = Report{ID: "r1", TargetUserID: "victim", State: domain.ReportOpen}
	store.users["victim"] = &UserSummary{ID: "victim", Username: "victim"}
	s := newSvc(store)

	// A viewer may read the queue but may not action a report or a user.
	if _, err := s.ListReports(ctx, who(domain.RoleViewer), 10); err != nil {
		t.Fatalf("viewer ListReports: %v", err)
	}
	if err := s.ResolveReport(ctx, who(domain.RoleViewer), "r1", domain.Dismiss, "spam"); statusOf(t, err) != 403 {
		t.Fatalf("viewer must not dismiss: %v", err)
	}
	if _, err := s.SearchUsers(ctx, who(domain.RoleViewer), "vic", 10); statusOf(t, err) != 403 {
		t.Fatalf("viewer must not search users: %v", err)
	}

	// An agent may dismiss/warn + search, but not suspend (operator-level).
	if _, err := s.SearchUsers(ctx, who(domain.RoleAgent), "vic", 10); err != nil {
		t.Fatalf("agent SearchUsers: %v", err)
	}
	if err := s.ResolveReport(ctx, who(domain.RoleAgent), "r1", domain.Suspend, "abuse"); statusOf(t, err) != 403 {
		t.Fatalf("agent must not suspend-via-resolve: %v", err)
	}
	if err := s.SuspendUser(ctx, who(domain.RoleAgent), "victim", "abuse"); statusOf(t, err) != 403 {
		t.Fatalf("agent must not suspend directly: %v", err)
	}

	// Only the owner reads the audit log.
	if _, err := s.ListAudit(ctx, who(domain.RoleOperator), 10); statusOf(t, err) != 403 {
		t.Fatalf("operator must not read audit: %v", err)
	}
	if _, err := s.ListAudit(ctx, who(domain.RoleOwner), 10); err != nil {
		t.Fatalf("owner ListAudit: %v", err)
	}
}

// ── audit-on-mutation (security-architecture §4) ─────────────────────────

func TestMutationAlwaysAudited(t *testing.T) {
	ctx := context.Background()

	t.Run("resolve-with-suspend: report actioned + target suspended + one audit row", func(t *testing.T) {
		store := newStore()
		store.reports["r1"] = Report{ID: "r1", TargetUserID: "victim", State: domain.ReportOpen}
		store.users["victim"] = &UserSummary{ID: "victim", Username: "victim"}
		s := newSvc(store)

		if err := s.ResolveReport(ctx, who(domain.RoleOperator), "r1", domain.Suspend, "harassment"); err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got := store.reports["r1"].State; got != domain.ReportActioned {
			t.Fatalf("report state = %v, want actioned", got)
		}
		if got := store.users["victim"].Status; got != 1 {
			t.Fatalf("victim status = %d, want 1 (suspended)", got)
		}
		if len(store.audit) != 1 {
			t.Fatalf("want exactly one audit row, got %d", len(store.audit))
		}
		a := store.audit[0]
		if a.Action != "report.suspend" || a.Target != "r1" || a.Reason != "harassment" || a.Actor != "admin-operator" {
			t.Fatalf("audit row = %+v", a)
		}
	})

	t.Run("direct suspend + reactivate each leave an audit row", func(t *testing.T) {
		store := newStore()
		store.users["u"] = &UserSummary{ID: "u", Username: "u"}
		s := newSvc(store)
		op := who(domain.RoleOperator)

		if err := s.SuspendUser(ctx, op, "u", "ban evasion"); err != nil {
			t.Fatal(err)
		}
		if err := s.ReactivateUser(ctx, op, "u", "appeal upheld"); err != nil {
			t.Fatal(err)
		}
		if store.users["u"].Status != 0 {
			t.Fatalf("status = %d, want 0 (reactivated)", store.users["u"].Status)
		}
		if len(store.audit) != 2 {
			t.Fatalf("want 2 audit rows, got %d", len(store.audit))
		}
		if store.audit[0].Action != "user.suspend" || store.audit[1].Action != "user.reactivate" {
			t.Fatalf("audit actions = %q, %q", store.audit[0].Action, store.audit[1].Action)
		}
	})
}

// ── validation + not-found ───────────────────────────────────────────────

func TestValidationAndNotFound(t *testing.T) {
	ctx := context.Background()
	store := newStore()
	store.reports["open"] = Report{ID: "open", TargetUserID: "v", State: domain.ReportOpen}
	store.reports["done"] = Report{ID: "done", State: domain.ReportDismissed}
	store.users["v"] = &UserSummary{ID: "v", Username: "v"}
	s := newSvc(store)
	agent, op := who(domain.RoleAgent), who(domain.RoleOperator)

	t.Run("reason required", func(t *testing.T) {
		if err := s.ResolveReport(ctx, agent, "open", domain.Dismiss, "  "); statusOf(t, err) != 400 {
			t.Fatalf("blank reason must be 400: %v", err)
		}
		if err := s.SuspendUser(ctx, op, "v", ""); statusOf(t, err) != 400 {
			t.Fatalf("blank suspend reason must be 400: %v", err)
		}
	})
	t.Run("unknown resolution", func(t *testing.T) {
		if err := s.ResolveReport(ctx, agent, "open", domain.Resolution("delete-account"), "x"); statusOf(t, err) != 400 {
			t.Fatalf("bad resolution must be 400: %v", err)
		}
	})
	t.Run("empty search query", func(t *testing.T) {
		if _, err := s.SearchUsers(ctx, agent, "   ", 10); statusOf(t, err) != 400 {
			t.Fatalf("empty query must be 400: %v", err)
		}
	})
	t.Run("missing report / user → 404", func(t *testing.T) {
		if _, err := s.GetReport(ctx, agent, "nope"); statusOf(t, err) != 404 {
			t.Fatalf("missing report must be 404: %v", err)
		}
		if _, err := s.UserMetadata(ctx, agent, "nobody"); statusOf(t, err) != 404 {
			t.Fatalf("missing user must be 404: %v", err)
		}
	})
	t.Run("already-resolved report → 404, no second audit", func(t *testing.T) {
		if err := s.ResolveReport(ctx, agent, "done", domain.Dismiss, "x"); statusOf(t, err) != 404 {
			t.Fatalf("re-resolving must be 404: %v", err)
		}
		if len(store.audit) != 0 {
			t.Fatalf("a rejected action must not audit, got %d rows", len(store.audit))
		}
	})
}
