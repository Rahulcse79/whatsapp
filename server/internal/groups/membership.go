package groups

import "context"

// MemberSource returns the authoritative member set + group version from the
// system of record (PostgreSQL). The pg Store implements it.
type MemberSource interface {
	MembersAndVersion(ctx context.Context, groupID string) ([]string, int64, error)
}

// MembershipCache caches versioned member sets for fan-out (Valkey
// group_members:{id} set + group_ver:{id}; data-structures-algorithms.md §12).
type MembershipCache interface {
	Get(ctx context.Context, groupID string) (members []string, version int64, hit bool, err error)
	Put(ctx context.Context, groupID string, members []string, version int64) error
	Invalidate(ctx context.Context, groupID string) error
}

// Membership is the fan-out worker's read path: it resolves a group's members
// at a version ≥ minVersion, reloading from the source when the cache is
// missing or stale. Ordering of group.events on the per-group NATS subject
// bounds the stale-read window (§12).
type Membership struct {
	cache  MembershipCache
	source MemberSource
}

func NewMembership(cache MembershipCache, source MemberSource) *Membership {
	return &Membership{cache: cache, source: source}
}

// Members returns the member set at a version ≥ minVersion. The fan-out worker
// passes the triggering group.event version as minVersion, so a cache that has
// not yet caught up to the event is bypassed and reloaded (§12).
func (m *Membership) Members(ctx context.Context, groupID string, minVersion int64) ([]string, int64, error) {
	if members, version, hit, err := m.cache.Get(ctx, groupID); err == nil && hit && version >= minVersion {
		return members, version, nil
	}
	members, version, err := m.source.MembersAndVersion(ctx, groupID)
	if err != nil {
		return nil, 0, err
	}
	_ = m.cache.Put(ctx, groupID, members, version) // best-effort warm; reload heals
	return members, version, nil
}

// OnMembershipEvent invalidates the cache when a membership event arrives.
// Proactive — Members also self-heals via the version compare, so a dropped
// invalidation costs at most one extra reload.
func (m *Membership) OnMembershipEvent(ctx context.Context, groupID string) {
	_ = m.cache.Invalidate(ctx, groupID)
}
