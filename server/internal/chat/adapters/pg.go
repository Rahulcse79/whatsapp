// Package adapters implements the chat context's persistence and dedupe
// ports. The accept transaction is the system's hottest write path.
package adapters

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/chat"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// Store implements chat.Store over PostgreSQL.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Accept assigns the next conversation seq and writes one inbox row per
// recipient device, in a single transaction.
//
// Query plans (the hot path — reviewed per coding-standards.md):
//   - membership check: index-only on conversation_members PK
//   - seq bump: UPDATE conversations by PK (id), RETURNING — one row lock,
//     which serializes ordering per conversation
//   - recipient resolution: conversation_members_by_user? no — filtered by
//     conversation_id (PK prefix) joined to devices.devices_by_user
//   - inbox insert: batched, keyed by message_inbox PK (no scans)
func (s *Store) Accept(ctx context.Context, p chat.AcceptParams) (chat.StoreResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return chat.StoreResult{}, fmt.Errorf("begin accept tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Sender must be a member of the conversation.
	var isMember bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id = $1 AND user_id = $2)`,
		p.ConversationID, p.SenderUserID).Scan(&isMember); err != nil {
		return chat.StoreResult{}, fmt.Errorf("membership check: %w", err)
	}
	if !isMember {
		return chat.StoreResult{}, chat.ErrNotMember
	}

	// Assign the next per-conversation sequence (row lock = total order).
	var seq int64
	if err := tx.QueryRow(ctx,
		`UPDATE conversations SET seq = seq + 1 WHERE id = $1 RETURNING seq`,
		p.ConversationID).Scan(&seq); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return chat.StoreResult{}, fmt.Errorf("conversation %s not found", p.ConversationID)
		}
		return chat.StoreResult{}, fmt.Errorf("assigning seq: %w", err)
	}

	// Recipients = every active device of every member, minus the sending
	// device (the sender's OTHER devices are included for self-sync).
	rows, err := tx.Query(ctx, `
		SELECT d.id
		FROM conversation_members cm
		JOIN devices d ON d.user_id = cm.user_id
		WHERE cm.conversation_id = $1 AND d.revoked_at IS NULL AND d.id <> $2`,
		p.ConversationID, p.SenderDeviceID)
	if err != nil {
		return chat.StoreResult{}, fmt.Errorf("resolving recipients: %w", err)
	}
	var deviceIDs []string
	for rows.Next() {
		var did string
		if err := rows.Scan(&did); err != nil {
			rows.Close()
			return chat.StoreResult{}, fmt.Errorf("scanning recipient: %w", err)
		}
		deviceIDs = append(deviceIDs, did)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return chat.StoreResult{}, fmt.Errorf("iterating recipients: %w", err)
	}

	// Batched inbox insert. ON CONFLICT DO NOTHING makes the write idempotent
	// at the row level (belt-and-suspenders with the Valkey dedupe).
	// NOTE: for large groups (≤1024 members × devices) this moves to a
	// COPY-based fan-out worker off the ACK path (T1.03, DS&A §4).
	// NULL when absent; a validated UUIDv7 string otherwise (pgx encodes it to
	// the uuid column). The service validates the format before we get here.
	var overlay any
	if p.OverlayTarget != "" {
		overlay = p.OverlayTarget
	}
	batch := &pgx.Batch{}
	for _, did := range deviceIDs {
		batch.Queue(`
			INSERT INTO message_inbox
			  (recipient_device_id, conversation_id, seq, msg_uuid, sender_device_id,
			   kind, overlay_target, ciphertext, accepted_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT DO NOTHING`,
			did, p.ConversationID, seq, p.MsgUUID, p.SenderDeviceID,
			int16(p.Kind), overlay, p.Ciphertext, p.AcceptedAt, p.ExpiresAt)
	}
	br := tx.SendBatch(ctx, batch)
	for range deviceIDs {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return chat.StoreResult{}, fmt.Errorf("inbox insert: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return chat.StoreResult{}, fmt.Errorf("closing inbox batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return chat.StoreResult{}, fmt.Errorf("commit accept tx: %w", err)
	}
	return chat.StoreResult{Seq: seq, RecipientCount: len(deviceIDs)}, nil
}

// GetOrCreateDirect returns the id of the 1:1 conversation between two users,
// creating it (and its membership) if absent. Idempotent on the user pair via
// the direct_key unique index.
func (s *Store) GetOrCreateDirect(ctx context.Context, userA, userB string) (string, error) {
	dk := directKey(userA, userB)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin conversation tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	newID := id.New()
	if _, err := tx.Exec(ctx,
		`INSERT INTO conversations (id, kind, direct_key) VALUES ($1, 0, $2)
		 ON CONFLICT (direct_key) DO NOTHING`, newID, dk); err != nil {
		return "", fmt.Errorf("creating direct conversation: %w", err)
	}

	var convID string
	if err := tx.QueryRow(ctx,
		`SELECT id FROM conversations WHERE direct_key = $1`, dk).Scan(&convID); err != nil {
		return "", fmt.Errorf("loading direct conversation: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO conversation_members (conversation_id, user_id)
		VALUES ($1, $2), ($1, $3) ON CONFLICT DO NOTHING`,
		convID, userA, userB); err != nil {
		return "", fmt.Errorf("adding members: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit conversation tx: %w", err)
	}
	return convID, nil
}

// directKey is the order-independent identity of a 1:1 conversation.
func directKey(a, b string) string {
	if a <= b {
		return a + ":" + b
	}
	return b + ":" + a
}
