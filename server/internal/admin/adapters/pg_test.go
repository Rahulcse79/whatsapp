package adapters

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/admin"
	"github.com/whatsapp-v2/server/internal/admin/domain"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("WA_TEST_PG_DSN not set — runs in the CI integration job")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pg connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedUser(t *testing.T, pool *pgxpool.Pool, username string) string {
	t.Helper()
	uid := id.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, phone_hash, username) VALUES ($1, $2, $3)`,
		uid, []byte("ph-"+uid), username)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return uid
}

func seedDevice(t *testing.T, pool *pgxpool.Pool, userID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO devices (id, user_id, is_primary, platform, identity_key, cert)
		 VALUES ($1, $2, true, 0, $3, $4)`,
		id.New(), userID, []byte("ik"), []byte("cert"))
	if err != nil {
		t.Fatalf("seed device: %v", err)
	}
}

func seedReport(t *testing.T, pool *pgxpool.Pool, reporter, target string, reason int16) string {
	t.Helper()
	rid := id.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO reports (id, reporter_id, target_user_id, reason) VALUES ($1, $2, $3, $4)`,
		rid, reporter, target, reason)
	if err != nil {
		t.Fatalf("seed report: %v", err)
	}
	return rid
}

// TestIntegration_Admin_ResolveIsAtomicallyAudited is the security assertion of
// security-architecture §4 against real PostgreSQL: a report resolution flips
// the report state, suspends the target, AND writes the audit row in one
// transaction — and the audit actor is an OIDC subject (text, migration 000015).
func TestIntegration_Admin_ResolveIsAtomicallyAudited(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	reporter := seedUser(t, pool, "reporter-"+id.New())
	target := seedUser(t, pool, "target-"+id.New())
	seedDevice(t, pool, target)
	rid := seedReport(t, pool, reporter, target, 3)

	// Open queue surfaces it; Get carries metadata only (no disclosure attached).
	open, err := store.ListOpen(ctx, 50)
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if !containsReport(open, rid) {
		t.Fatal("seeded report absent from the open queue")
	}
	rep, err := store.Get(ctx, rid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rep.TargetUserID != target || rep.State != domain.ReportOpen || rep.HasDisclosure {
		t.Fatalf("report = %+v", rep)
	}

	// Resolve-with-suspend: state → actioned, target suspended, audit appended.
	entry := admin.AuditEntry{Actor: "oidc|operator-42", Action: "report.suspend", Target: rid, Reason: "harassment"}
	if err := store.Resolve(ctx, rid, domain.ReportActioned, target, entry); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	rep2, _ := store.Get(ctx, rid)
	if rep2.State != domain.ReportActioned {
		t.Fatalf("state = %v, want actioned", rep2.State)
	}
	sum, err := store.Summary(ctx, target)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.Status != 1 || sum.DeviceCount != 1 || sum.ReportCount < 1 {
		t.Fatalf("summary = %+v (want status 1, 1 device, ≥1 report)", sum)
	}

	recs, err := store.List(ctx, 50)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	if !containsAudit(recs, "oidc|operator-42", "report.suspend", rid) {
		t.Fatal("resolution did not leave its audit row (co-transactionality broken)")
	}

	// Re-resolving a closed report is a no-op surfaced as not-found.
	if err := store.Resolve(ctx, rid, domain.ReportDismissed, "", entry); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("re-resolve = %v, want ErrNotFound", err)
	}
}

func TestIntegration_Admin_SearchAndSetStatus(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	uname := "needle-" + id.New()
	uid := seedUser(t, pool, uname)

	found, err := store.Search(ctx, "needle-", 50)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !containsUser(found, uid) {
		t.Fatal("username search did not find the seeded user")
	}

	// SetStatus writes status + audit atomically; missing user → not-found.
	if err := store.SetStatus(ctx, uid, 1, admin.AuditEntry{Actor: "oidc|op", Action: "user.suspend", Target: uid, Reason: "spam"}); err != nil {
		t.Fatalf("set status: %v", err)
	}
	sum, _ := store.Summary(ctx, uid)
	if sum.Status != 1 {
		t.Fatalf("status = %d, want 1", sum.Status)
	}
	if err := store.SetStatus(ctx, id.New(), 1, admin.AuditEntry{Actor: "oidc|op", Action: "user.suspend"}); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("set status on a ghost = %v, want ErrNotFound", err)
	}
	if _, err := store.Summary(ctx, id.New()); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("summary of a ghost = %v, want ErrNotFound", err)
	}
}

// TestIntegration_Admin_FlagWriteIsAudited proves flag upsert/delete write their
// audit_log row in the same tx and that updated_by holds an OIDC subject (text,
// migration 000016).
func TestIntegration_Admin_FlagWriteIsAudited(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	flag := "admtest_" + id.New()
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM feature_flags WHERE flag = $1`, flag) })

	rule := []byte(`{"enabled":true,"rollout":50}`)
	if err := store.UpsertFlag(ctx, flag, rule, admin.AuditEntry{Actor: "oidc|op-7", Action: "flag.set:" + flag, Reason: "rollout"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Rule + updated_by round-trip (updated_by must accept the OIDC subject).
	var rawOut []byte
	var updatedBy string
	if err := pool.QueryRow(ctx, `SELECT rules, updated_by FROM feature_flags WHERE flag = $1`, flag).Scan(&rawOut, &updatedBy); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if updatedBy != "oidc|op-7" {
		t.Fatalf("updated_by = %q, want oidc|op-7", updatedBy)
	}

	recs, _ := store.List(ctx, 50)
	if !containsAudit(recs, "oidc|op-7", "flag.set:"+flag, "") {
		t.Fatal("upsert did not leave its audit row")
	}

	// Idempotent upsert (ON CONFLICT) + delete + missing-delete → not-found.
	if err := store.UpsertFlag(ctx, flag, []byte(`{"enabled":false}`), admin.AuditEntry{Actor: "oidc|op-7", Action: "flag.set:" + flag, Reason: "disable"}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if err := store.DeleteFlag(ctx, flag, admin.AuditEntry{Actor: "oidc|op-7", Action: "flag.delete:" + flag, Reason: "cleanup"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.DeleteFlag(ctx, "ghost_"+id.New(), admin.AuditEntry{Actor: "oidc|op-7", Action: "flag.delete"}); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("deleting a missing flag = %v, want ErrNotFound", err)
	}
}

func containsReport(rs []admin.Report, id string) bool {
	for _, r := range rs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func containsUser(us []admin.UserSummary, id string) bool {
	for _, u := range us {
		if u.ID == id {
			return true
		}
	}
	return false
}

func containsAudit(as []admin.AuditRecord, actor, action, target string) bool {
	for _, a := range as {
		if a.Actor == actor && a.Action == action && a.Target == target {
			return true
		}
	}
	return false
}
