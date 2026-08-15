package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/communities"
	"github.com/whatsapp-v2/server/internal/communities/domain"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("WA_TEST_PG_DSN not set — runs in the CI migrations job")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	uid := id.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, phone_hash) VALUES ($1, $2)`, uid, []byte("ph-"+uid)); err != nil {
		t.Fatal(err)
	}
	return uid
}

func TestIntegration_CommunityLifecycle(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	owner := seedUser(t, pool)
	member := seedUser(t, pool)

	c := communities.Community{
		ID: id.New(), Name: "Integration Devs", Description: "hi", Kind: domain.KindPublic,
		OwnerID: owner, AnnouncementGroupID: id.New(), CreatedAt: time.Now(),
	}
	if err := s.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(ctx, c.ID, owner, domain.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(ctx, c.ID, member, domain.RoleMember); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, c.ID)
	if err != nil || got.Name != "Integration Devs" || got.Kind != domain.KindPublic {
		t.Fatalf("get: %v %+v", err, got)
	}

	// role change + membership read
	if err := s.SetRole(ctx, c.ID, member, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMember(ctx, c.ID, member)
	if err != nil || m.Role != domain.RoleAdmin {
		t.Fatalf("member role: %v %+v", err, m)
	}

	// group link + list
	gid := id.New()
	if err := s.AddGroup(ctx, c.ID, gid); err != nil {
		t.Fatal(err)
	}
	if gs, err := s.ListGroups(ctx, c.ID); err != nil || len(gs) != 1 || gs[0] != gid {
		t.Fatalf("groups: %v %v", err, gs)
	}

	// counts (2 members, 1 group)
	mc, gc, err := s.Counts(ctx, c.ID)
	if err != nil || mc != 2 || gc != 1 {
		t.Fatalf("counts: %v members=%d groups=%d", err, mc, gc)
	}

	// event create + list + delete
	ev := communities.Event{
		ID: id.New(), CommunityID: c.ID, Title: "Standup", StartsAt: time.Now().Add(time.Hour),
		CreatedBy: owner, CreatedAt: time.Now(),
	}
	if err := s.CreateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if es, err := s.ListEvents(ctx, c.ID, time.Now().Add(-time.Hour)); err != nil || len(es) != 1 {
		t.Fatalf("events: %v %v", err, es)
	}
	if err := s.DeleteEvent(ctx, c.ID, ev.ID); err != nil {
		t.Fatal(err)
	}

	// discover finds the public community
	found := false
	list, err := s.Discover(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, sm := range list {
		if sm.ID == c.ID {
			found = true
			if sm.MemberCount != 2 {
				t.Fatalf("discover member count = %d, want 2", sm.MemberCount)
			}
		}
	}
	if !found {
		t.Fatalf("discover did not include the public community")
	}

	// remove member, then delete community (cascades members/groups/events)
	if err := s.RemoveMember(ctx, c.ID, member); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, c.ID); err != communities.ErrNotFound {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
}
