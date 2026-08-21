package collab

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/collab/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

const activityLimit = 100

// Service runs the collaboration control plane. Every action is gated on the
// caller being a member of the note/task's conversation.
type Service struct {
	store Store
	now   func() time.Time
	newID func() string
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now, newID: id.New}
}

// ── notes ────────────────────────────────────────────────────────────────────

func (s *Service) CreateNote(ctx context.Context, ident auth.Identity, conversationID, title, body string) (NoteView, error) {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return NoteView{}, err
	}
	if err := domain.ValidateNote(title, body); err != nil {
		return NoteView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_NOTE", err.Error())
	}
	now := s.now()
	n := Note{
		ID: s.newID(), ConversationID: conversationID, Title: strings.TrimSpace(title), Body: body,
		Version: 1, Approval: domain.ApprovalNone, CreatedBy: ident.UserID, UpdatedBy: ident.UserID, UpdatedAt: now,
	}
	if err := s.store.CreateNote(ctx, n); err != nil {
		return NoteView{}, httpx.Transient()
	}
	_ = s.store.AddRevision(ctx, Revision{ID: s.newID(), NoteID: n.ID, Version: 1, Title: n.Title, Body: n.Body, Author: ident.UserID, CreatedAt: now})
	s.log(ctx, conversationID, ident.UserID, "note.create", "created “"+n.Title+"”")
	return noteView(n), nil
}

func (s *Service) Notes(ctx context.Context, ident auth.Identity, conversationID string) ([]NoteView, error) {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return nil, err
	}
	ns, err := s.store.ListNotes(ctx, conversationID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]NoteView, len(ns))
	for i, n := range ns {
		out[i] = noteView(n)
	}
	return out, nil
}

// UpdateNote applies an edit under optimistic concurrency: base must equal the
// note's current version, else 409 (the client reloads + rebases).
func (s *Service) UpdateNote(ctx context.Context, ident auth.Identity, noteID, title, body string, base int) (NoteView, error) {
	n, err := s.loadNoteMember(ctx, ident, noteID)
	if err != nil {
		return NoteView{}, err
	}
	if err := domain.ValidateNote(title, body); err != nil {
		return NoteView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_NOTE", err.Error())
	}
	next, err := domain.CheckVersion(n.Version, base)
	if err != nil {
		return NoteView{}, httpx.Reject(http.StatusConflict, "CONFLICT_STALE", err.Error())
	}
	now := s.now()
	if err := s.store.UpdateNote(ctx, noteID, strings.TrimSpace(title), body, next, ident.UserID, now); err != nil {
		return NoteView{}, httpx.Transient()
	}
	_ = s.store.AddRevision(ctx, Revision{ID: s.newID(), NoteID: noteID, Version: next, Title: strings.TrimSpace(title), Body: body, Author: ident.UserID, CreatedAt: now})
	s.log(ctx, n.ConversationID, ident.UserID, "note.edit", "edited “"+strings.TrimSpace(title)+"”")
	n.Title, n.Body, n.Version, n.UpdatedBy, n.UpdatedAt = strings.TrimSpace(title), body, next, ident.UserID, now
	return noteView(n), nil
}

func (s *Service) Revisions(ctx context.Context, ident auth.Identity, noteID string) ([]RevisionView, error) {
	if _, err := s.loadNoteMember(ctx, ident, noteID); err != nil {
		return nil, err
	}
	rs, err := s.store.ListRevisions(ctx, noteID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]RevisionView, len(rs))
	for i, r := range rs {
		out[i] = RevisionView{Version: r.Version, Title: r.Title, Body: r.Body, Author: r.Author, CreatedAtMS: r.CreatedAt.UnixMilli()}
	}
	return out, nil
}

// ── approvals ────────────────────────────────────────────────────────────────

func (s *Service) RequestApproval(ctx context.Context, ident auth.Identity, noteID string) error {
	n, err := s.loadNoteMember(ctx, ident, noteID)
	if err != nil {
		return err
	}
	if !domain.CanRequestApproval(n.Approval) {
		return httpx.Reject(http.StatusConflict, "STATE_APPROVAL", "this note is already awaiting or has approval")
	}
	if err := s.store.SetApproval(ctx, noteID, domain.ApprovalPending, nil, s.now()); err != nil {
		return httpx.Transient()
	}
	s.log(ctx, n.ConversationID, ident.UserID, "note.approval.request", "requested approval on “"+n.Title+"”")
	return nil
}

func (s *Service) DecideApproval(ctx context.Context, ident auth.Identity, noteID string, approve bool) error {
	n, err := s.loadNoteMember(ctx, ident, noteID)
	if err != nil {
		return err
	}
	next, err := domain.DecideApproval(n.Approval, approve)
	if err != nil {
		return httpx.Reject(http.StatusConflict, "STATE_APPROVAL", "this note is not awaiting approval")
	}
	approver := ident.UserID
	if err := s.store.SetApproval(ctx, noteID, next, &approver, s.now()); err != nil {
		return httpx.Transient()
	}
	verb := "approved"
	if !approve {
		verb = "rejected"
	}
	s.log(ctx, n.ConversationID, ident.UserID, "note.approval."+verb, verb+" “"+n.Title+"”")
	return nil
}

// ── comments ─────────────────────────────────────────────────────────────────

func (s *Service) AddComment(ctx context.Context, ident auth.Identity, noteID, body string) (CommentView, error) {
	n, err := s.loadNoteMember(ctx, ident, noteID)
	if err != nil {
		return CommentView{}, err
	}
	if err := domain.ValidateComment(body); err != nil {
		return CommentView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_COMMENT", err.Error())
	}
	c := Comment{ID: s.newID(), NoteID: noteID, Author: ident.UserID, Body: strings.TrimSpace(body), CreatedAt: s.now()}
	if err := s.store.AddComment(ctx, c); err != nil {
		return CommentView{}, httpx.Transient()
	}
	s.log(ctx, n.ConversationID, ident.UserID, "note.comment", "commented on “"+n.Title+"”")
	return CommentView{ID: c.ID, Author: c.Author, Body: c.Body, CreatedAtMS: c.CreatedAt.UnixMilli()}, nil
}

func (s *Service) Comments(ctx context.Context, ident auth.Identity, noteID string) ([]CommentView, error) {
	if _, err := s.loadNoteMember(ctx, ident, noteID); err != nil {
		return nil, err
	}
	cs, err := s.store.ListComments(ctx, noteID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]CommentView, len(cs))
	for i, c := range cs {
		out[i] = CommentView{ID: c.ID, Author: c.Author, Body: c.Body, CreatedAtMS: c.CreatedAt.UnixMilli()}
	}
	return out, nil
}

// ── tasks ────────────────────────────────────────────────────────────────────

func (s *Service) CreateTask(ctx context.Context, ident auth.Identity, conversationID, title string) (TaskView, error) {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return TaskView{}, err
	}
	if err := domain.ValidateTask(title); err != nil {
		return TaskView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_TASK", err.Error())
	}
	t := Task{ID: s.newID(), ConversationID: conversationID, Title: strings.TrimSpace(title), CreatedBy: ident.UserID, CreatedAt: s.now()}
	if err := s.store.CreateTask(ctx, t); err != nil {
		return TaskView{}, httpx.Transient()
	}
	s.log(ctx, conversationID, ident.UserID, "task.add", "added task “"+t.Title+"”")
	return TaskView{ID: t.ID, Title: t.Title, CreatedAtMS: t.CreatedAt.UnixMilli()}, nil
}

func (s *Service) Tasks(ctx context.Context, ident auth.Identity, conversationID string) ([]TaskView, error) {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return nil, err
	}
	ts, err := s.store.ListTasks(ctx, conversationID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]TaskView, len(ts))
	for i, t := range ts {
		out[i] = TaskView{ID: t.ID, Title: t.Title, Done: t.Done, Assignee: t.Assignee, CreatedAtMS: t.CreatedAt.UnixMilli()}
	}
	return out, nil
}

func (s *Service) ToggleTask(ctx context.Context, ident auth.Identity, conversationID, taskID string, done bool) error {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.SetTaskDone(ctx, conversationID, taskID, done); err != nil {
		return httpx.Transient()
	}
	verb := "completed"
	if !done {
		verb = "reopened"
	}
	s.log(ctx, conversationID, ident.UserID, "task.done", verb+" a task")
	return nil
}

func (s *Service) AssignTask(ctx context.Context, ident auth.Identity, conversationID, taskID string, assignee *string) error {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.AssignTask(ctx, conversationID, taskID, assignee); err != nil {
		return httpx.Transient()
	}
	return nil
}

func (s *Service) DeleteTask(ctx context.Context, ident auth.Identity, conversationID, taskID string) error {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.DeleteTask(ctx, conversationID, taskID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── activity timeline ────────────────────────────────────────────────────────

func (s *Service) Activity(ctx context.Context, ident auth.Identity, conversationID string) ([]ActivityView, error) {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return nil, err
	}
	as, err := s.store.ListActivity(ctx, conversationID, activityLimit)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]ActivityView, len(as))
	for i, a := range as {
		out[i] = ActivityView{Actor: a.Actor, Kind: a.Kind, Summary: a.Summary, AtMS: a.At.UnixMilli()}
	}
	return out, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func (s *Service) requireMember(ctx context.Context, conversationID, userID string) error {
	ok, err := s.store.IsMember(ctx, conversationID, userID)
	if err != nil {
		return httpx.Transient()
	}
	if !ok {
		return notFound()
	}
	return nil
}

func (s *Service) loadNoteMember(ctx context.Context, ident auth.Identity, noteID string) (Note, error) {
	n, err := s.store.GetNote(ctx, noteID)
	if errors.Is(err, ErrNotFound) {
		return Note{}, notFound()
	}
	if err != nil {
		return Note{}, httpx.Transient()
	}
	if err := s.requireMember(ctx, n.ConversationID, ident.UserID); err != nil {
		return Note{}, err
	}
	return n, nil
}

func (s *Service) log(ctx context.Context, conversationID, actor, kind, summary string) {
	_ = s.store.AddActivity(ctx, Activity{ID: s.newID(), ConversationID: conversationID, Actor: actor, Kind: kind, Summary: summary, At: s.now()})
}

func noteView(n Note) NoteView {
	return NoteView{
		ID: n.ID, Title: n.Title, Body: n.Body, Version: n.Version, Approval: n.Approval.String(),
		Approver: n.Approver, UpdatedBy: n.UpdatedBy, UpdatedAtMS: n.UpdatedAt.UnixMilli(),
	}
}

func notFound() error {
	return httpx.Reject(http.StatusNotFound, "COLLAB_NOT_FOUND", "not found")
}
