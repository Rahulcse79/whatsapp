package collab

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/collab/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

type fakeStore struct {
	members   map[string]map[string]bool // convID → userID → member
	notes     map[string]Note
	revisions map[string][]Revision
	tasks     map[string]Task
	comments  map[string][]Comment
	activity  []Activity
}

func newFake() *fakeStore {
	return &fakeStore{
		members: map[string]map[string]bool{}, notes: map[string]Note{},
		revisions: map[string][]Revision{}, tasks: map[string]Task{}, comments: map[string][]Comment{},
	}
}

func (s *fakeStore) IsMember(_ context.Context, convID, userID string) (bool, error) {
	return s.members[convID][userID], nil
}
func (s *fakeStore) CreateNote(_ context.Context, n Note) error { s.notes[n.ID] = n; return nil }
func (s *fakeStore) GetNote(_ context.Context, id string) (Note, error) {
	n, ok := s.notes[id]
	if !ok {
		return Note{}, ErrNotFound
	}
	return n, nil
}
func (s *fakeStore) ListNotes(_ context.Context, convID string) ([]Note, error) {
	var out []Note
	for _, n := range s.notes {
		if n.ConversationID == convID {
			out = append(out, n)
		}
	}
	return out, nil
}
func (s *fakeStore) UpdateNote(_ context.Context, id, title, body string, version int, updatedBy string, at time.Time) error {
	n := s.notes[id]
	n.Title, n.Body, n.Version, n.UpdatedBy, n.UpdatedAt = title, body, version, updatedBy, at
	s.notes[id] = n
	return nil
}
func (s *fakeStore) SetApproval(_ context.Context, id string, state domain.ApprovalState, approver *string, _ time.Time) error {
	n := s.notes[id]
	n.Approval, n.Approver = state, approver
	s.notes[id] = n
	return nil
}
func (s *fakeStore) AddRevision(_ context.Context, r Revision) error {
	s.revisions[r.NoteID] = append(s.revisions[r.NoteID], r)
	return nil
}
func (s *fakeStore) ListRevisions(_ context.Context, noteID string) ([]Revision, error) {
	return s.revisions[noteID], nil
}
func (s *fakeStore) CreateTask(_ context.Context, t Task) error { s.tasks[t.ID] = t; return nil }
func (s *fakeStore) ListTasks(_ context.Context, convID string) ([]Task, error) {
	var out []Task
	for _, t := range s.tasks {
		if t.ConversationID == convID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (s *fakeStore) SetTaskDone(_ context.Context, _, taskID string, done bool) error {
	t := s.tasks[taskID]
	t.Done = done
	s.tasks[taskID] = t
	return nil
}
func (s *fakeStore) AssignTask(_ context.Context, _, taskID string, assignee *string) error {
	t := s.tasks[taskID]
	t.Assignee = assignee
	s.tasks[taskID] = t
	return nil
}
func (s *fakeStore) DeleteTask(_ context.Context, _, taskID string) error {
	delete(s.tasks, taskID)
	return nil
}
func (s *fakeStore) AddComment(_ context.Context, c Comment) error {
	s.comments[c.NoteID] = append(s.comments[c.NoteID], c)
	return nil
}
func (s *fakeStore) ListComments(_ context.Context, noteID string) ([]Comment, error) {
	return s.comments[noteID], nil
}
func (s *fakeStore) AddActivity(_ context.Context, a Activity) error {
	s.activity = append(s.activity, a)
	return nil
}
func (s *fakeStore) ListActivity(_ context.Context, convID string, _ int) ([]Activity, error) {
	var out []Activity
	for i := len(s.activity) - 1; i >= 0; i-- {
		if s.activity[i].ConversationID == convID {
			out = append(out, s.activity[i])
		}
	}
	return out, nil
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

func newSvc() (*Service, *fakeStore) {
	store := newFake()
	store.members["c1"] = map[string]bool{"alice": true, "bob": true}
	svc := NewService(store)
	n := 0
	svc.newID = func() string { n++; return fmt.Sprintf("id%d", n) }
	svc.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return svc, store
}

func TestNoteVersionedEdits(t *testing.T) {
	svc, store := newSvc()
	// non-member can't create
	if _, err := svc.CreateNote(context.Background(), who("mallory"), "c1", "Plan", "x"); codeOf(t, err) != "COLLAB_NOT_FOUND" {
		t.Fatal("non-member create should 404")
	}
	nv, err := svc.CreateNote(context.Background(), who("alice"), "c1", "Plan", "v1 body")
	if err != nil || nv.Version != 1 {
		t.Fatalf("create: %v %+v", err, nv)
	}
	// stale edit conflicts
	if _, err := svc.UpdateNote(context.Background(), who("bob"), nv.ID, "Plan", "bad", 0); codeOf(t, err) != "CONFLICT_STALE" {
		t.Fatal("stale base should 409")
	}
	// in-sync edit bumps the version + records a revision
	up, err := svc.UpdateNote(context.Background(), who("bob"), nv.ID, "Plan v2", "v2 body", 1)
	if err != nil || up.Version != 2 {
		t.Fatalf("update: %v %+v", err, up)
	}
	revs, _ := svc.Revisions(context.Background(), who("alice"), nv.ID)
	if len(revs) != 2 || revs[1].Version != 2 {
		t.Fatalf("revisions: %+v", revs)
	}
	_ = store
}

func TestApprovalFlow(t *testing.T) {
	svc, _ := newSvc()
	nv, _ := svc.CreateNote(context.Background(), who("alice"), "c1", "Budget", "b")
	if err := svc.RequestApproval(context.Background(), who("alice"), nv.ID); err != nil {
		t.Fatalf("request: %v", err)
	}
	// re-request while pending conflicts
	if err := svc.RequestApproval(context.Background(), who("alice"), nv.ID); codeOf(t, err) != "STATE_APPROVAL" {
		t.Fatal("re-request pending should 409")
	}
	if err := svc.DecideApproval(context.Background(), who("bob"), nv.ID, true); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// deciding an already-approved note conflicts
	if err := svc.DecideApproval(context.Background(), who("bob"), nv.ID, false); codeOf(t, err) != "STATE_APPROVAL" {
		t.Fatal("decide approved should 409")
	}
	notes, _ := svc.Notes(context.Background(), who("alice"), "c1")
	if notes[0].Approval != "approved" || notes[0].Approver == nil || *notes[0].Approver != "bob" {
		t.Fatalf("approval state: %+v", notes[0])
	}
}

func TestTasksAndComments(t *testing.T) {
	svc, _ := newSvc()
	tv, err := svc.CreateTask(context.Background(), who("alice"), "c1", "Buy cake")
	if err != nil || tv.Done {
		t.Fatalf("create task: %v %+v", err, tv)
	}
	if err := svc.ToggleTask(context.Background(), who("bob"), "c1", tv.ID, true); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	tasks, _ := svc.Tasks(context.Background(), who("alice"), "c1")
	if len(tasks) != 1 || !tasks[0].Done {
		t.Fatalf("tasks: %+v", tasks)
	}
	nv, _ := svc.CreateNote(context.Background(), who("alice"), "c1", "Plan", "b")
	if _, err := svc.AddComment(context.Background(), who("bob"), nv.ID, "looks good"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	cs, _ := svc.Comments(context.Background(), who("alice"), nv.ID)
	if len(cs) != 1 || cs[0].Body != "looks good" {
		t.Fatalf("comments: %+v", cs)
	}
}

func TestActivityTimeline(t *testing.T) {
	svc, _ := newSvc()
	nv, _ := svc.CreateNote(context.Background(), who("alice"), "c1", "Plan", "b")
	_, _ = svc.UpdateNote(context.Background(), who("bob"), nv.ID, "Plan", "b2", 1)
	acts, err := svc.Activity(context.Background(), who("alice"), "c1")
	if err != nil || len(acts) < 2 {
		t.Fatalf("activity: %v %+v", err, acts)
	}
	// newest first
	if acts[0].Kind != "note.edit" || acts[0].Actor != "bob" {
		t.Fatalf("latest activity: %+v", acts[0])
	}
}
