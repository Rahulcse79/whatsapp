package adapters

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/discovery/domain"
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

func TestIntegration_DiscoverySearch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	owner := id.New()
	tok := "zqx" + id.New()[:6] // a unique token so this test only matches its own rows
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, phone_hash, username) VALUES ($1, $2, $3)`,
		owner, []byte("ph-"+owner), tok+"_alice"); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// public + private channels
	exec(`INSERT INTO channels (id, owner_id, handle, name, kind, verified) VALUES ($1,$2,$3,$4,0,true)`, id.New(), owner, tok+"pub", "Public "+tok)
	exec(`INSERT INTO channels (id, owner_id, handle, name, kind) VALUES ($1,$2,$3,$4,1)`, id.New(), owner, tok+"prv", "Private "+tok)
	// public + private communities
	exec(`INSERT INTO communities (id, name, kind, owner_id, announcement_group_id) VALUES ($1,$2,0,$3,$4)`, id.New(), tok+" Community", owner, id.New())
	exec(`INSERT INTO communities (id, name, kind, owner_id, announcement_group_id) VALUES ($1,$2,1,$3,$4)`, id.New(), "Private "+tok, owner, id.New())

	b := NewBackend(pool)
	res, err := b.Search(ctx, strings.ToLower(tok), nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[domain.Kind]int{}
	for _, r := range res {
		if strings.Contains(strings.ToLower(r.Title+r.Handle), strings.ToLower(tok)) {
			kinds[r.Kind]++
		}
		if strings.Contains(strings.ToLower(r.Title), "private") {
			t.Fatalf("private entity leaked into discovery: %+v", r)
		}
	}
	if kinds[domain.KindChannel] != 1 || kinds[domain.KindCommunity] != 1 || kinds[domain.KindUser] != 1 {
		t.Fatalf("expected one public hit per kind, got %+v", kinds)
	}

	// reindex source enumerates the same public docs (excludes privates)
	docs, err := NewSource(pool).AllDocs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mine := 0
	for _, d := range docs {
		if strings.Contains(strings.ToLower(d.Title+d.Handle), strings.ToLower(tok)) {
			if strings.Contains(strings.ToLower(d.Title), "private") {
				t.Fatalf("private doc in reindex feed: %+v", d)
			}
			mine++
		}
	}
	if mine != 3 {
		t.Fatalf("reindex feed should carry 3 public docs for this token, got %d", mine)
	}
}
