// Package collab is the collaboration control plane (T12.01): shared notes with
// versioned optimistic-concurrency edits + revision history, shared task lists,
// comments, an approval workflow, and an activity timeline — scoped to a
// conversation and gated on its membership. Live cursor presence + full CRDT
// character-merge ride the existing presence plane / a documented seam; the
// version-gated edit model + revision log are the substance.
package collab

import (
	"context"
	"errors"
	"time"

	"github.com/whatsapp-v2/server/internal/collab/domain"
)

var ErrNotFound = errors.New("collab: not found")

// Note is a shared note.
type Note struct {
	ID             string
	ConversationID string
	Title          string
	Body           string
	Version        int
	Approval       domain.ApprovalState
	Approver       *string
	CreatedBy      string
	UpdatedBy      string
	UpdatedAt      time.Time
}

// Revision is one historical version of a note.
type Revision struct {
	ID        string
	NoteID    string
	Version   int
	Title     string
	Body      string
	Author    string
	CreatedAt time.Time
}

// Task is a shared task-list item.
type Task struct {
	ID             string
	ConversationID string
	Title          string
	Done           bool
	Assignee       *string
	CreatedBy      string
	CreatedAt      time.Time
}

// Comment is a comment on a note.
type Comment struct {
	ID        string
	NoteID    string
	Author    string
	Body      string
	CreatedAt time.Time
}

// Activity is one entry of the activity timeline.
type Activity struct {
	ID             string
	ConversationID string
	Actor          string
	Kind           string
	Summary        string
	At             time.Time
}

// ── wire views ───────────────────────────────────────────────────────────────

type NoteView struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	Version     int     `json:"version"`
	Approval    string  `json:"approval"`
	Approver    *string `json:"approver,omitempty"`
	UpdatedBy   string  `json:"updated_by"`
	UpdatedAtMS int64   `json:"updated_at_ms"`
}

type RevisionView struct {
	Version     int    `json:"version"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Author      string `json:"author"`
	CreatedAtMS int64  `json:"created_at_ms"`
}

type TaskView struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Done        bool    `json:"done"`
	Assignee    *string `json:"assignee,omitempty"`
	CreatedAtMS int64   `json:"created_at_ms"`
}

type CommentView struct {
	ID          string `json:"id"`
	Author      string `json:"author"`
	Body        string `json:"body"`
	CreatedAtMS int64  `json:"created_at_ms"`
}

type ActivityView struct {
	Actor   string `json:"actor"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	AtMS    int64  `json:"at_ms"`
}

// Store persists collab notes/tasks/comments/activity and gates on membership.
type Store interface {
	IsMember(ctx context.Context, conversationID, userID string) (bool, error)

	CreateNote(ctx context.Context, n Note) error
	GetNote(ctx context.Context, noteID string) (Note, error) // ErrNotFound
	ListNotes(ctx context.Context, conversationID string) ([]Note, error)
	UpdateNote(ctx context.Context, noteID, title, body string, version int, updatedBy string, at time.Time) error
	SetApproval(ctx context.Context, noteID string, state domain.ApprovalState, approver *string, at time.Time) error

	AddRevision(ctx context.Context, r Revision) error
	ListRevisions(ctx context.Context, noteID string) ([]Revision, error)

	CreateTask(ctx context.Context, t Task) error
	ListTasks(ctx context.Context, conversationID string) ([]Task, error)
	SetTaskDone(ctx context.Context, conversationID, taskID string, done bool) error
	AssignTask(ctx context.Context, conversationID, taskID string, assignee *string) error
	DeleteTask(ctx context.Context, conversationID, taskID string) error

	AddComment(ctx context.Context, c Comment) error
	ListComments(ctx context.Context, noteID string) ([]Comment, error)

	AddActivity(ctx context.Context, a Activity) error
	ListActivity(ctx context.Context, conversationID string, limit int) ([]Activity, error)
}
