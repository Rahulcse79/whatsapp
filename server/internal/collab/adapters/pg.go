// Package adapters implements collab.Store over PostgreSQL (collab_notes +
// collab_note_revisions + collab_tasks + collab_comments + collab_activity;
// migration 000027) with membership via conversation_members.
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/collab"
	"github.com/whatsapp-v2/server/internal/collab/domain"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) IsMember(ctx context.Context, conversationID, userID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM conversation_members WHERE conversation_id = $1 AND user_id = $2)`,
		conversationID, userID).Scan(&ok)
	return ok, err
}

func (s *Store) CreateNote(ctx context.Context, n collab.Note) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO collab_notes (id, conversation_id, title, body, version, approval_state, created_by, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		n.ID, n.ConversationID, n.Title, n.Body, n.Version, int16(n.Approval), n.CreatedBy, n.UpdatedBy, n.UpdatedAt)
	return err
}

func (s *Store) GetNote(ctx context.Context, id string) (collab.Note, error) {
	var n collab.Note
	var state int16
	err := s.pool.QueryRow(ctx,
		`SELECT id, conversation_id, title, body, version, approval_state, approver, created_by, updated_by, updated_at
		 FROM collab_notes WHERE id = $1`, id).
		Scan(&n.ID, &n.ConversationID, &n.Title, &n.Body, &n.Version, &state, &n.Approver, &n.CreatedBy, &n.UpdatedBy, &n.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return collab.Note{}, collab.ErrNotFound
	}
	n.Approval = domain.ApprovalState(state)
	return n, err
}

func (s *Store) ListNotes(ctx context.Context, conversationID string) ([]collab.Note, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, conversation_id, title, body, version, approval_state, approver, created_by, updated_by, updated_at
		 FROM collab_notes WHERE conversation_id = $1 ORDER BY updated_at DESC`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []collab.Note
	for rows.Next() {
		var n collab.Note
		var state int16
		if err := rows.Scan(&n.ID, &n.ConversationID, &n.Title, &n.Body, &n.Version, &state, &n.Approver, &n.CreatedBy, &n.UpdatedBy, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Approval = domain.ApprovalState(state)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) UpdateNote(ctx context.Context, noteID, title, body string, version int, updatedBy string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE collab_notes SET title = $2, body = $3, version = $4, updated_by = $5, updated_at = $6 WHERE id = $1`,
		noteID, title, body, version, updatedBy, at)
	return err
}

func (s *Store) SetApproval(ctx context.Context, noteID string, state domain.ApprovalState, approver *string, at time.Time) error {
	var decidedAt *time.Time
	if approver != nil { // a decision (approve/reject) — record when
		decidedAt = &at
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE collab_notes SET approval_state = $2, approver = $3, decided_at = $4 WHERE id = $1`,
		noteID, int16(state), approver, decidedAt)
	return err
}

func (s *Store) AddRevision(ctx context.Context, r collab.Revision) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO collab_note_revisions (id, note_id, version, title, body, author, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		r.ID, r.NoteID, r.Version, r.Title, r.Body, r.Author, r.CreatedAt)
	return err
}

func (s *Store) ListRevisions(ctx context.Context, noteID string) ([]collab.Revision, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, note_id, version, title, body, author, created_at
		 FROM collab_note_revisions WHERE note_id = $1 ORDER BY version`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []collab.Revision
	for rows.Next() {
		var r collab.Revision
		if err := rows.Scan(&r.ID, &r.NoteID, &r.Version, &r.Title, &r.Body, &r.Author, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateTask(ctx context.Context, t collab.Task) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO collab_tasks (id, conversation_id, title, done, assignee, created_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.ID, t.ConversationID, t.Title, t.Done, t.Assignee, t.CreatedBy, t.CreatedAt)
	return err
}

func (s *Store) ListTasks(ctx context.Context, conversationID string) ([]collab.Task, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, conversation_id, title, done, assignee, created_by, created_at
		 FROM collab_tasks WHERE conversation_id = $1 ORDER BY done, created_at`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []collab.Task
	for rows.Next() {
		var t collab.Task
		if err := rows.Scan(&t.ID, &t.ConversationID, &t.Title, &t.Done, &t.Assignee, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SetTaskDone(ctx context.Context, conversationID, taskID string, done bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE collab_tasks SET done = $3 WHERE id = $1 AND conversation_id = $2`, taskID, conversationID, done)
	return err
}

func (s *Store) AssignTask(ctx context.Context, conversationID, taskID string, assignee *string) error {
	_, err := s.pool.Exec(ctx, `UPDATE collab_tasks SET assignee = $3 WHERE id = $1 AND conversation_id = $2`, taskID, conversationID, assignee)
	return err
}

func (s *Store) DeleteTask(ctx context.Context, conversationID, taskID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM collab_tasks WHERE id = $1 AND conversation_id = $2`, taskID, conversationID)
	return err
}

func (s *Store) AddComment(ctx context.Context, c collab.Comment) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO collab_comments (id, note_id, author, body, created_at) VALUES ($1, $2, $3, $4, $5)`,
		c.ID, c.NoteID, c.Author, c.Body, c.CreatedAt)
	return err
}

func (s *Store) ListComments(ctx context.Context, noteID string) ([]collab.Comment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, note_id, author, body, created_at FROM collab_comments WHERE note_id = $1 ORDER BY created_at`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []collab.Comment
	for rows.Next() {
		var c collab.Comment
		if err := rows.Scan(&c.ID, &c.NoteID, &c.Author, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddActivity(ctx context.Context, a collab.Activity) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO collab_activity (id, conversation_id, actor, kind, summary, at) VALUES ($1, $2, $3, $4, $5, $6)`,
		a.ID, a.ConversationID, a.Actor, a.Kind, a.Summary, a.At)
	return err
}

func (s *Store) ListActivity(ctx context.Context, conversationID string, limit int) ([]collab.Activity, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, conversation_id, actor, kind, summary, at
		 FROM collab_activity WHERE conversation_id = $1 ORDER BY at DESC LIMIT $2`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []collab.Activity
	for rows.Next() {
		var a collab.Activity
		if err := rows.Scan(&a.ID, &a.ConversationID, &a.Actor, &a.Kind, &a.Summary, &a.At); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
