package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/collab"
	"github.com/whatsapp-v2/server/internal/collab/domain"
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

func TestIntegration_Collab(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)

	alice := seedUser(t, pool)
	bob := seedUser(t, pool)
	convID := id.New()
	if _, err := pool.Exec(ctx, `INSERT INTO conversations (id, kind) VALUES ($1, 0)`, convID); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{alice, bob} {
		if _, err := pool.Exec(ctx, `INSERT INTO conversation_members (conversation_id, user_id) VALUES ($1, $2)`, convID, u); err != nil {
			t.Fatal(err)
		}
	}
	if ok, _ := s.IsMember(ctx, convID, alice); !ok {
		t.Fatal("alice should be a member")
	}
	if ok, _ := s.IsMember(ctx, convID, id.New()); ok {
		t.Fatal("stranger is not a member")
	}

	// note + revision + version bump
	now := time.Now()
	note := collab.Note{ID: id.New(), ConversationID: convID, Title: "Plan", Body: "v1", Version: 1, CreatedBy: alice, UpdatedBy: alice, UpdatedAt: now}
	if err := s.CreateNote(ctx, note); err != nil {
		t.Fatal(err)
	}
	if err := s.AddRevision(ctx, collab.Revision{ID: id.New(), NoteID: note.ID, Version: 1, Title: "Plan", Body: "v1", Author: alice, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateNote(ctx, note.ID, "Plan v2", "v2", 2, bob, now); err != nil {
		t.Fatal(err)
	}
	if err := s.AddRevision(ctx, collab.Revision{ID: id.New(), NoteID: note.ID, Version: 2, Title: "Plan v2", Body: "v2", Author: bob, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNote(ctx, note.ID)
	if err != nil || got.Version != 2 || got.Title != "Plan v2" {
		t.Fatalf("note after update: %v %+v", err, got)
	}
	revs, _ := s.ListRevisions(ctx, note.ID)
	if len(revs) != 2 || revs[0].Version != 1 || revs[1].Version != 2 {
		t.Fatalf("revisions: %+v", revs)
	}

	// approval
	if err := s.SetApproval(ctx, note.ID, domain.ApprovalPending, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetApproval(ctx, note.ID, domain.ApprovalApproved, &bob, now); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetNote(ctx, note.ID)
	if got2.Approval != domain.ApprovalApproved || got2.Approver == nil || *got2.Approver != bob {
		t.Fatalf("approval: %+v", got2)
	}

	// comment
	if err := s.AddComment(ctx, collab.Comment{ID: id.New(), NoteID: note.ID, Author: bob, Body: "lgtm", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	cs, _ := s.ListComments(ctx, note.ID)
	if len(cs) != 1 || cs[0].Body != "lgtm" {
		t.Fatalf("comments: %+v", cs)
	}

	// task lifecycle
	task := collab.Task{ID: id.New(), ConversationID: convID, Title: "Buy cake", CreatedBy: alice, CreatedAt: now}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskDone(ctx, convID, task.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignTask(ctx, convID, task.ID, &bob); err != nil {
		t.Fatal(err)
	}
	tasks, _ := s.ListTasks(ctx, convID)
	if len(tasks) != 1 || !tasks[0].Done || tasks[0].Assignee == nil || *tasks[0].Assignee != bob {
		t.Fatalf("tasks: %+v", tasks)
	}

	// activity timeline
	if err := s.AddActivity(ctx, collab.Activity{ID: id.New(), ConversationID: convID, Actor: alice, Kind: "note.create", Summary: "created", At: now}); err != nil {
		t.Fatal(err)
	}
	acts, _ := s.ListActivity(ctx, convID, 10)
	if len(acts) != 1 {
		t.Fatalf("activity: %+v", acts)
	}
}
