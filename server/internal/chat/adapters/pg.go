// Package adapters implements the chat context's persistence and dedupe
// ports. The accept transaction is the system's hottest write path.
package adapters

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	return chat.StoreResult{Seq: seq, RecipientDeviceIDs: deviceIDs}, nil
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
	// conversations_direct_key is a partial unique index (WHERE direct_key IS
	// NOT NULL — groups carry no direct_key); ON CONFLICT only infers a
	// partial index when the target repeats its predicate.
	if _, err := tx.Exec(ctx,
		`INSERT INTO conversations (id, kind, direct_key) VALUES ($1, 0, $2)
		 ON CONFLICT (direct_key) WHERE direct_key IS NOT NULL DO NOTHING`, newID, dk); err != nil {
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

// zeroUUID is the keyset floor for the replay scan (uuid sorts bytewise).
const zeroUUID = "00000000-0000-0000-0000-000000000000"

// PullInbox streams the device's pending items in primary-key order —
// (recipient_device_id, conversation_id, seq) — using keyset pagination, so
// each batch is one bounded index scan and a mid-replay disconnect resumes
// from the client's cursors, never from scratch.
//
// The per-conversation cursor filter runs in Go: acked rows are deleted, so
// cursors only skip the narrow window where a client persisted items whose
// ack has not landed yet — rarely more than one batch's worth.
func (s *Store) PullInbox(ctx context.Context, deviceID string, cursors []chat.Cursor, batchSize int, emit func(batch []chat.InboxItem) error) error {
	after := make(map[string]int64, len(cursors))
	for _, c := range cursors {
		after[c.ConversationID] = c.LastSeq
	}

	lastConv, lastSeq := zeroUUID, int64(0)
	for {
		// LEFT JOIN: a sender device deleted with its user must not black-hole
		// the ciphertext — the item replays with an empty sender_user_id.
		rows, err := s.pool.Query(ctx, `
			SELECT mi.conversation_id, mi.seq, mi.msg_uuid,
			       COALESCE(d.user_id::text, ''), mi.sender_device_id, mi.kind,
			       COALESCE(mi.overlay_target::text, ''), mi.ciphertext, mi.accepted_at
			FROM message_inbox mi
			LEFT JOIN devices d ON d.id = mi.sender_device_id
			WHERE mi.recipient_device_id = $1
			  AND (mi.conversation_id, mi.seq) > ($2::uuid, $3)
			ORDER BY mi.conversation_id, mi.seq
			LIMIT $4`, deviceID, lastConv, lastSeq, batchSize)
		if err != nil {
			return fmt.Errorf("replay scan: %w", err)
		}

		var (
			batch   = make([]chat.InboxItem, 0, batchSize)
			scanned int
		)
		for rows.Next() {
			var (
				it         chat.InboxItem
				kind       int16
				acceptedAt time.Time
			)
			if err := rows.Scan(&it.ConversationID, &it.Seq, &it.MsgUUID,
				&it.SenderUserID, &it.SenderDeviceID, &kind,
				&it.OverlayTarget, &it.Ciphertext, &acceptedAt); err != nil {
				rows.Close()
				return fmt.Errorf("replay scan row: %w", err)
			}
			scanned++
			lastConv, lastSeq = it.ConversationID, it.Seq
			if it.Seq <= after[it.ConversationID] {
				continue // client already persisted this one
			}
			it.Kind = chat.MsgKind(kind)
			it.AcceptedAtMS = acceptedAt.UnixMilli()
			batch = append(batch, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("replay scan rows: %w", err)
		}

		if len(batch) > 0 {
			if err := emit(batch); err != nil {
				return err
			}
		}
		if scanned < batchSize {
			return nil // drained
		}
	}
}

// AckDelivered deletes every inbox row the client has durably persisted —
// cumulative per conversation, idempotent by construction (a DELETE with
// nothing left to delete is a no-op).
func (s *Store) AckDelivered(ctx context.Context, deviceID string, upTo []chat.Cursor) error {
	batch := &pgx.Batch{}
	for _, c := range upTo {
		batch.Queue(`
			DELETE FROM message_inbox
			WHERE recipient_device_id = $1 AND conversation_id = $2 AND seq <= $3`,
			deviceID, c.ConversationID, c.LastSeq)
	}
	br := s.pool.SendBatch(ctx, batch)
	for range upTo {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("ack delete: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("closing ack batch: %w", err)
	}
	return nil
}

var _ chat.Inbox = (*Store)(nil)
