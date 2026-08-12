package adapters

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/platform/id"
	"github.com/whatsapp-v2/server/internal/stories"
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

// seedUser inserts a bare user (stories.author_id / story_views.viewer_id FK).
func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	uid := id.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, phone_hash) VALUES ($1, $2)`, uid, []byte("ph-"+uid))
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return uid
}

func TestIntegration_Stories_PostFeedViewPurge(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	author := seedUser(t, pool)
	bob := seedUser(t, pool)
	mallory := seedUser(t, pool)
	now := time.Now()

	st := stories.Story{
		ID: id.New(), AuthorID: author, Audience: []string{author, bob},
		ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
	}
	if err := store.Create(ctx, st); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Feed: audience member sees it; a non-member does not.
	feed, err := store.Feed(ctx, bob, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 1 || feed[0].ID != st.ID {
		t.Fatalf("bob feed = %+v, want the story", feed)
	}
	if f, _ := store.Feed(ctx, mallory, now); len(f) != 0 {
		t.Fatalf("mallory feed = %+v, want empty", f)
	}

	// Record a view (idempotent) and read it back.
	if err := store.RecordView(ctx, st.ID, bob); err != nil {
		t.Fatal(err)
	}
	_ = store.RecordView(ctx, st.ID, bob)
	viewers, err := store.Viewers(ctx, st.ID)
	if err != nil || len(viewers) != 1 || viewers[0].UserID != bob {
		t.Fatalf("viewers = %+v (%v), want [bob]", viewers, err)
	}

	// Expired feed is empty; purge removes the row (and cascades the view).
	if f, _ := store.Feed(ctx, bob, now.Add(25*time.Hour)); len(f) != 0 {
		t.Fatalf("expired feed = %+v, want empty", f)
	}
	n, err := store.PurgeExpired(ctx, now.Add(25*time.Hour))
	if err != nil || n < 1 {
		t.Fatalf("purge = (%d,%v), want ≥1", n, err)
	}
	if _, err := store.Get(ctx, st.ID); !errors.Is(err, stories.ErrNotFound) {
		t.Fatalf("get after purge = %v, want ErrNotFound", err)
	}
}
