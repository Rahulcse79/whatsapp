package flags

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeStore struct {
	rules map[string]Rule
	gets  int
	err   error
}

func (f *fakeStore) Get(_ context.Context, flag string) (Rule, bool, error) {
	f.gets++
	if f.err != nil {
		return Rule{}, false, f.err
	}
	r, ok := f.rules[flag]
	return r, ok, nil
}

func (f *fakeStore) List(context.Context) ([]Named, error) { return nil, nil }

type fakeCache struct {
	m          map[string]Entry
	dels, puts int
}

func newFakeCache() *fakeCache { return &fakeCache{m: map[string]Entry{}} }

func (c *fakeCache) Get(_ context.Context, flag string) (Entry, bool, error) {
	e, ok := c.m[flag]
	return e, ok, nil
}
func (c *fakeCache) Put(_ context.Context, flag string, e Entry) error {
	c.puts++
	c.m[flag] = e
	return nil
}
func (c *fakeCache) Del(_ context.Context, flag string) error {
	c.dels++
	delete(c.m, flag)
	return nil
}

func TestEnabledReadsThroughAndCaches(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rules: map[string]Rule{"new_ui": {Enabled: true, Rollout: 100}}}
	cache := newFakeCache()
	s := NewService(store, cache)

	if !s.Enabled(ctx, "new_ui", "u1") {
		t.Fatal("enabled flag reported off")
	}
	// Second read is served from cache — the store is not hit again.
	if !s.Enabled(ctx, "new_ui", "u1") {
		t.Fatal("second read reported off")
	}
	if store.gets != 1 {
		t.Fatalf("store hit %d times, want 1 (second read cached)", store.gets)
	}
}

func TestAbsentFlagIsNegativelyCached(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rules: map[string]Rule{}}
	cache := newFakeCache()
	s := NewService(store, cache)

	if s.Enabled(ctx, "ghost", "u1") {
		t.Fatal("absent flag reported on")
	}
	_ = s.Enabled(ctx, "ghost", "u1")
	if store.gets != 1 {
		t.Fatalf("absent flag hit store %d times, want 1 (negative cache)", store.gets)
	}
}

func TestEnabledFailsClosedOnStoreError(t *testing.T) {
	s := NewService(&fakeStore{err: errors.New("db down")}, newFakeCache())
	if s.Enabled(context.Background(), "any", "u1") {
		t.Fatal("a feature gate must fail closed when the store errors")
	}
}

func TestKillSwitchFailsOpen(t *testing.T) {
	ctx := context.Background()
	// Store errors → Enabled(kill flag) false → Allowed true: an outage must
	// never accidentally pause the product.
	s := NewService(&fakeStore{err: errors.New("db down")}, newFakeCache())
	if !s.Allowed(ctx, KillCalls, "") {
		t.Fatal("kill-switch must fail OPEN on store error")
	}

	// Engaged switch → not allowed.
	engaged := NewService(&fakeStore{rules: map[string]Rule{
		string(KillCalls): {Enabled: true, Rollout: 100},
	}}, newFakeCache())
	if engaged.Allowed(ctx, KillCalls, "") {
		t.Fatal("an engaged kill-switch must deny")
	}
}

func TestKillSwitchMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	store := &fakeStore{rules: map[string]Rule{
		string(KillCalls): {Enabled: true, Rollout: 100}, // calls paused
		// group creation not engaged
	}}
	mw := NewService(store, newFakeCache()).KillSwitchMiddleware(CoreAPIGuards())
	h := mw(next)

	do := func(method, path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}

	if got := do(http.MethodPost, "/v1/calls"); got != http.StatusServiceUnavailable {
		t.Errorf("paused call setup = %d, want 503", got)
	}
	if got := do(http.MethodPost, "/v1/groups"); got != http.StatusOK {
		t.Errorf("group creation (not paused) = %d, want 200", got)
	}
	// Method mismatch: the guard is POST-only, a GET passes through.
	if got := do(http.MethodGet, "/v1/calls"); got != http.StatusOK {
		t.Errorf("GET /v1/calls = %d, want 200 (guard is POST-only)", got)
	}
	// Unguarded route passes through.
	if got := do(http.MethodPost, "/v1/messages"); got != http.StatusOK {
		t.Errorf("unguarded route = %d, want 200", got)
	}
}
