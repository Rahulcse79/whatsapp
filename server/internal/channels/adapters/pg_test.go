package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/channels"
	"github.com/whatsapp-v2/server/internal/channels/domain"
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

func TestIntegration_ChannelLifecycle(t *testing.T) {
	pool := testPool(t)
	s := NewStore(pool)
	ctx := context.Background()

	owner := seedUser(t, pool)
	alice := seedUser(t, pool)
	handle := "chan" + id.New()[:8]

	ch := channels.Channel{
		ID: id.New(), OwnerID: owner, Handle: handle, Name: "News", Description: "d",
		Kind: domain.KindPublic, CreatedAt: time.Now().UTC(),
	}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Duplicate handle → ErrHandleTaken.
	dup := ch
	dup.ID = id.New()
	if err := s.CreateChannel(ctx, dup); err != channels.ErrHandleTaken {
		t.Fatalf("expected ErrHandleTaken, got %v", err)
	}
	// Owner + follower memberships → follower count 2.
	mustExec(t, s.AddMember(ctx, channels.Member{ChannelID: ch.ID, UserID: owner, Role: domain.RoleOwner, JoinedAt: time.Now()}))
	mustExec(t, s.AddMember(ctx, channels.Member{ChannelID: ch.ID, UserID: alice, Role: domain.RoleFollower, JoinedAt: time.Now()}))
	if n, _ := s.FollowerCount(ctx, ch.ID); n != 2 {
		t.Fatalf("follower count = %d, want 2", n)
	}
	// Promote alice.
	if err := s.SetRole(ctx, ch.ID, alice, domain.RoleAdmin); err != nil {
		t.Fatalf("setrole: %v", err)
	}
	if m, _ := s.GetMember(ctx, ch.ID, alice); m.Role != domain.RoleAdmin {
		t.Fatal("role not updated")
	}

	// A published post + a scheduled (future) post.
	now := time.Now().UTC()
	live := channels.Post{ID: id.New(), ChannelID: ch.ID, AuthorID: owner, Body: "live", PublishAt: now, Published: true, CreatedAt: now}
	sched := channels.Post{ID: id.New(), ChannelID: ch.ID, AuthorID: owner, Body: "later", PublishAt: now.Add(time.Hour), Published: false, CreatedAt: now}
	mustExec(t, s.CreatePost(ctx, live))
	mustExec(t, s.CreatePost(ctx, sched))
	if ps, _ := s.ListPosts(ctx, ch.ID, 10); len(ps) != 1 || ps[0].Body != "live" {
		t.Fatalf("feed should have only the live post, got %+v", ps)
	}
	// PublishDue after the scheduled time publishes the second one.
	if due, _ := s.PublishDue(ctx, now.Add(2*time.Hour), 10); len(due) != 1 || due[0].Body != "later" {
		t.Fatalf("PublishDue should return the scheduled post, got %+v", due)
	}
	if ps, _ := s.ListPosts(ctx, ch.ID, 10); len(ps) != 2 {
		t.Fatalf("both posts should now be live, got %d", len(ps))
	}

	// Reactions + comments aggregate.
	mustExec(t, s.React(ctx, live.ID, alice, "👍"))
	mustExec(t, s.React(ctx, live.ID, owner, "👍"))
	mustExec(t, s.React(ctx, live.ID, alice, "👍")) // idempotent for the same user
	if rx, _ := s.Reactions(ctx, live.ID); rx["👍"] != 2 {
		t.Fatalf("reaction tally = %d, want 2", rx["👍"])
	}
	cm := channels.Comment{ID: id.New(), PostID: live.ID, AuthorID: alice, Body: "nice", CreatedAt: now}
	mustExec(t, s.CreateComment(ctx, cm))
	if n, _ := s.CommentCount(ctx, live.ID); n != 1 {
		t.Fatalf("comment count = %d, want 1", n)
	}
	mustExec(t, s.DeleteComment(ctx, cm.ID))
	if n, _ := s.CommentCount(ctx, live.ID); n != 0 {
		t.Fatalf("comment count after delete = %d, want 0", n)
	}

	// Discover + search find the public channel.
	if cs, _ := s.SearchChannels(ctx, "News", 10); len(cs) == 0 {
		t.Fatal("search should find the public channel")
	}
	// Soft delete hides it.
	mustExec(t, s.DeleteChannel(ctx, ch.ID))
	if _, err := s.GetChannel(ctx, ch.ID); err != channels.ErrNotFound {
		t.Fatalf("deleted channel should be ErrNotFound, got %v", err)
	}
}

func mustExec(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
