package admin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whatsapp-v2/server/internal/admin/domain"
	"github.com/whatsapp-v2/server/internal/platform/flags"
)

// ── fakes (distinct from service_test.go's fakeStore) ────────────────────

type fakeFlagWrite struct {
	flags  map[string][]byte
	audits []AuditEntry
}

func newFlagWrite() *fakeFlagWrite { return &fakeFlagWrite{flags: map[string][]byte{}} }

func (f *fakeFlagWrite) UpsertFlag(_ context.Context, flag string, rules []byte, a AuditEntry) error {
	f.flags[flag] = rules
	f.audits = append(f.audits, a)
	return nil
}

func (f *fakeFlagWrite) DeleteFlag(_ context.Context, flag string, a AuditEntry) error {
	if _, ok := f.flags[flag]; !ok {
		return ErrNotFound
	}
	delete(f.flags, flag)
	f.audits = append(f.audits, a)
	return nil
}

type fakeFlagRead struct{ list []flags.Named }

func (f fakeFlagRead) Get(context.Context, string) (flags.Rule, bool, error) {
	return flags.Rule{}, false, nil
}
func (f fakeFlagRead) List(context.Context) ([]flags.Named, error) { return f.list, nil }

type fakeFlagCache struct{ dels []string }

func (c *fakeFlagCache) Get(context.Context, string) (flags.Entry, bool, error) {
	return flags.Entry{}, false, nil
}
func (c *fakeFlagCache) Put(context.Context, string, flags.Entry) error { return nil }
func (c *fakeFlagCache) Del(_ context.Context, flag string) error {
	c.dels = append(c.dels, flag)
	return nil
}

func newConsole() (*FlagConsole, *fakeFlagWrite, *fakeFlagCache) {
	w, c := newFlagWrite(), &fakeFlagCache{}
	return NewFlagConsole(fakeFlagRead{}, w, c), w, c
}

// ── tests ────────────────────────────────────────────────────────────────

func TestFlagConsoleRBAC(t *testing.T) {
	ctx := context.Background()
	console, _, _ := newConsole()

	if _, err := console.List(ctx, who(domain.RoleViewer)); statusOf(t, err) != 403 {
		t.Fatalf("viewer must not list flags: %v", err)
	}
	if _, err := console.List(ctx, who(domain.RoleAgent)); err != nil {
		t.Fatalf("agent List: %v", err)
	}
	if err := console.Set(ctx, who(domain.RoleAgent), "f", flags.Rule{Enabled: true}, "why"); statusOf(t, err) != 403 {
		t.Fatalf("agent must not set flags (operator-level): %v", err)
	}
}

func TestFlagConsoleSetWritesAndBusts(t *testing.T) {
	ctx := context.Background()
	console, write, cache := newConsole()

	rule := flags.Rule{Enabled: true, Rollout: 100}
	if err := console.Set(ctx, who(domain.RoleOperator), string(flags.KillCalls), rule, "incident #42"); err != nil {
		t.Fatalf("operator Set: %v", err)
	}

	raw, ok := write.flags[string(flags.KillCalls)]
	if !ok {
		t.Fatal("flag was not written")
	}
	var got flags.Rule
	if err := json.Unmarshal(raw, &got); err != nil || !got.Enabled || got.Rollout != 100 {
		t.Fatalf("stored rule = %s (err %v)", raw, err)
	}
	if len(write.audits) != 1 || write.audits[0].Action != "flag.set:"+string(flags.KillCalls) || write.audits[0].Reason != "incident #42" {
		t.Fatalf("audit = %+v", write.audits)
	}
	if len(cache.dels) != 1 || cache.dels[0] != string(flags.KillCalls) {
		t.Fatalf("cache not busted: %+v", cache.dels)
	}
}

func TestFlagConsoleValidation(t *testing.T) {
	ctx := context.Background()
	console, _, _ := newConsole()
	op := who(domain.RoleOperator)

	if err := console.Set(ctx, op, "", flags.Rule{}, "r"); statusOf(t, err) != 400 {
		t.Errorf("empty flag name must be 400: %v", err)
	}
	if err := console.Set(ctx, op, "f", flags.Rule{}, "  "); statusOf(t, err) != 400 {
		t.Errorf("blank reason must be 400: %v", err)
	}
	if err := console.Set(ctx, op, "f", flags.Rule{Rollout: 101}, "r"); statusOf(t, err) != 400 {
		t.Errorf("rollout > 100 must be 400: %v", err)
	}
	if err := console.Set(ctx, op, "f", flags.Rule{Rollout: -1}, "r"); statusOf(t, err) != 400 {
		t.Errorf("negative rollout must be 400: %v", err)
	}
}

func TestFlagConsoleDelete(t *testing.T) {
	ctx := context.Background()
	console, write, cache := newConsole()
	op := who(domain.RoleOperator)

	write.flags["doomed"] = []byte(`{"enabled":true}`)
	if err := console.Delete(ctx, op, "doomed", "cleanup"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := write.flags["doomed"]; ok {
		t.Fatal("flag not deleted")
	}
	if len(cache.dels) == 0 || cache.dels[len(cache.dels)-1] != "doomed" {
		t.Fatal("cache not busted on delete")
	}
	// Missing flag → 404.
	if err := console.Delete(ctx, op, "ghost", "cleanup"); statusOf(t, err) != 404 {
		t.Fatalf("deleting a missing flag must be 404: %v", err)
	}
}
