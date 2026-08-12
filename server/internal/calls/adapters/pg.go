package adapters

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/calls"
	"github.com/whatsapp-v2/server/internal/calls/domain"
)

// History implements calls.History over call_records (metadata only, 90-day
// retention). The record id is the ring id (UUIDv7 → time-ordered), so history
// pages by a simple id keyset cursor and Upsert is idempotent per call.
type History struct{ pool *pgxpool.Pool }

func NewHistory(pool *pgxpool.Pool) *History { return &History{pool: pool} }

func (h *History) Upsert(ctx context.Context, rec calls.CallRecord) error {
	_, err := h.pool.Exec(ctx,
		`INSERT INTO call_records (id, room_id, kind, initiator, participants, started_at, ended_at, outcome)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (id) DO UPDATE SET ended_at = EXCLUDED.ended_at, outcome = EXCLUDED.outcome`,
		rec.ID, rec.RoomID, kindToDB(rec.Kind), rec.Initiator, rec.Participants, rec.StartedAt, rec.EndedAt, int16(rec.Outcome))
	return err
}

func (h *History) List(ctx context.Context, userID, cursor string, limit int) ([]calls.CallRecord, string, error) {
	const cols = `id, room_id, kind, initiator, participants, started_at, ended_at, outcome`
	var (
		rows pgx.Rows
		err  error
	)
	if cursor == "" {
		rows, err = h.pool.Query(ctx,
			`SELECT `+cols+` FROM call_records WHERE $1 = ANY(participants) ORDER BY id DESC LIMIT $2`,
			userID, limit+1)
	} else {
		rows, err = h.pool.Query(ctx,
			`SELECT `+cols+` FROM call_records WHERE $1 = ANY(participants) AND id < $2 ORDER BY id DESC LIMIT $3`,
			userID, cursor, limit+1)
	}
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []calls.CallRecord
	for rows.Next() {
		var (
			rec  calls.CallRecord
			kind int16
			oc   int16
		)
		if err := rows.Scan(&rec.ID, &rec.RoomID, &kind, &rec.Initiator, &rec.Participants, &rec.StartedAt, &rec.EndedAt, &oc); err != nil {
			return nil, "", err
		}
		rec.Kind = kindFromDB(kind)
		rec.Outcome = calls.Outcome(oc)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(out) > limit {
		next = out[limit].ID
		out = out[:limit]
	}
	return out, next, nil
}

// PurgeOlderThan deletes call_records started before cutoff (FR-CALL-06
// retention). The calls_by_time index on started_at serves the scan. Returns the
// number of rows removed.
func (h *History) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := h.pool.Exec(ctx, `DELETE FROM call_records WHERE started_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// call_records.kind is 0 voice | 1 video | 2 ptt (schema); domain.CallKind is
// aligned to the wire enum (voice 1, video 2), so shift by one across the seam.
func kindToDB(k domain.CallKind) int16   { return int16(k) - 1 }
func kindFromDB(k int16) domain.CallKind { return domain.CallKind(k + 1) }

var _ calls.History = (*History)(nil)

// Devices implements calls.Devices — resolves each user's non-revoked device ids.
type Devices struct{ pool *pgxpool.Pool }

func NewDevices(pool *pgxpool.Pool) *Devices { return &Devices{pool: pool} }

func (d *Devices) DevicesOf(ctx context.Context, userIDs []string) (map[string][]string, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT user_id, id FROM devices WHERE user_id = ANY($1) AND revoked_at IS NULL`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]string, len(userIDs))
	for rows.Next() {
		var userID, deviceID string
		if err := rows.Scan(&userID, &deviceID); err != nil {
			return nil, err
		}
		out[userID] = append(out[userID], deviceID)
	}
	return out, rows.Err()
}

var _ calls.Devices = (*Devices)(nil)
