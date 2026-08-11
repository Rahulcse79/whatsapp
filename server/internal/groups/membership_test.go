package groups

import (
	"context"
	"testing"
)

type memCache struct {
	entries map[string]struct {
		members []string
		version int64
	}
}

func newMemCache() *memCache {
	return &memCache{entries: map[string]struct {
		members []string
		version int64
	}{}}
}

func (c *memCache) Get(_ context.Context, gid string) ([]string, int64, bool, error) {
	e, ok := c.entries[gid]
	if !ok {
		return nil, 0, false, nil
	}
	return e.members, e.version, true, nil
}
func (c *memCache) Put(_ context.Context, gid string, members []string, version int64) error {
	c.entries[gid] = struct {
		members []string
		version int64
	}{members, version}
	return nil
}
func (c *memCache) Invalidate(_ context.Context, gid string) error {
	delete(c.entries, gid)
	return nil
}

type fakeSource struct {
	members []string
	version int64
	calls   int
}

func (s *fakeSource) MembersAndVersion(_ context.Context, _ string) ([]string, int64, error) {
	s.calls++
	return s.members, s.version, nil
}

func TestMembership_MissReloadsAndWarms(t *testing.T) {
	cache := newMemCache()
	src := &fakeSource{members: []string{"a", "b"}, version: 3}
	m := NewMembership(cache, src)

	members, ver, err := m.Members(context.Background(), "g1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || ver != 3 {
		t.Fatalf("miss reload wrong: %v v%d", members, ver)
	}
	if src.calls != 1 {
		t.Fatalf("source called %d times, want 1", src.calls)
	}
	// warmed → next read at the same version is served from cache
	if _, _, err := m.Members(context.Background(), "g1", 3); err != nil {
		t.Fatal(err)
	}
	if src.calls != 1 {
		t.Fatal("warm cache was not used")
	}
}

func TestMembership_StaleVersionReloads(t *testing.T) {
	cache := newMemCache()
	src := &fakeSource{members: []string{"a"}, version: 5}
	m := NewMembership(cache, src)

	// seed the cache at v5
	if _, _, err := m.Members(context.Background(), "g1", 5); err != nil {
		t.Fatal(err)
	}
	// a membership event bumped the group to v6 → the v5 cache is stale
	src.version = 6
	src.members = []string{"a", "c"}
	members, ver, err := m.Members(context.Background(), "g1", 6)
	if err != nil {
		t.Fatal(err)
	}
	if ver != 6 || len(members) != 2 {
		t.Fatalf("stale read not reloaded: %v v%d", members, ver)
	}
	if src.calls != 2 {
		t.Fatalf("source called %d, want 2 (seed + stale reload)", src.calls)
	}
}

func TestMembership_InvalidateForcesReload(t *testing.T) {
	cache := newMemCache()
	src := &fakeSource{members: []string{"a"}, version: 1}
	m := NewMembership(cache, src)

	if _, _, err := m.Members(context.Background(), "g1", 1); err != nil {
		t.Fatal(err)
	}
	m.OnMembershipEvent(context.Background(), "g1")
	if _, _, err := m.Members(context.Background(), "g1", 1); err != nil {
		t.Fatal(err)
	}
	if src.calls != 2 {
		t.Fatalf("invalidate did not force a reload: calls=%d", src.calls)
	}
}
