package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/abuse"
	"github.com/whatsapp-v2/server/internal/abuse/domain"
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

func TestIntegration_FileReport(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	reporter := seedUser(t, pool)
	target := seedUser(t, pool)

	if ok, err := s.UserExists(ctx, target); err != nil || !ok {
		t.Fatalf("target should exist: %v %v", ok, err)
	}
	if ok, _ := s.UserExists(ctx, id.New()); ok {
		t.Fatal("random id should not exist")
	}

	rep := abuse.Report{
		ID: id.New(), ReporterID: reporter, TargetUserID: target,
		Reason: domain.ReasonScam, Note: "phishing", DisclosedCiphertext: []byte("sealed"), CreatedAt: time.Now(),
	}
	if err := s.FileReport(ctx, rep); err != nil {
		t.Fatal(err)
	}

	// it lands as an open report (state 0) the admin queue will drain
	var state int16
	var note string
	if err := pool.QueryRow(ctx, `SELECT state, note FROM reports WHERE id = $1`, rep.ID).Scan(&state, &note); err != nil {
		t.Fatal(err)
	}
	if state != 0 || note != "phishing" {
		t.Fatalf("report row: state=%d note=%q", state, note)
	}
}
