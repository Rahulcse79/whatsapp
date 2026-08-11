// Package fanout is the group fan-out worker (core-api-lld.md §3, HLD §8.3).
// A group send is accepted + ACKed synchronously (the sender's durable intent
// row + seq), then this worker asynchronously expands membership from the
// Valkey versioned cache, batch-INSERTs one inbox row per recipient device
// (500 rows/statement), and publishes per-device deliveries as each batch
// commits. Ciphertext is encrypted ONCE with the sender's Sender Key (e2ee §3)
// and fanned unchanged — this package never inspects it.
package fanout

import (
	"context"
	"log/slog"
)

// BatchSize is the inbox INSERT batch (core-api-lld §3).
const BatchSize = 500

// FanoutJob is one accepted group message to fan out.
type FanoutJob struct {
	GroupID        string
	ConversationID string
	Seq            int64
	MsgUUID        string
	SenderUserID   string
	SenderDeviceID string
	Kind           int16
	Ciphertext     []byte
	AcceptedAtMS   int64
	ExpiresAtMS    int64
	// Version is the group version at accept time; passed to the membership
	// cache as minVersion so a cache lagging the send is bypassed (§12).
	Version int64
}

// InboxRow is one recipient device's delivery unit (a message_inbox row).
type InboxRow struct {
	DeviceID       string
	ConversationID string
	Seq            int64
	MsgUUID        string
	SenderUserID   string
	SenderDeviceID string
	Kind           int16
	Ciphertext     []byte
	AcceptedAtMS   int64
	ExpiresAtMS    int64
}

// MemberResolver returns a group's member user IDs at a version ≥ minVersion
// (satisfied by groups.Membership over the Valkey versioned cache).
type MemberResolver interface {
	Members(ctx context.Context, groupID string, minVersion int64) ([]string, int64, error)
}

// DeviceResolver expands member users to their active device IDs.
type DeviceResolver interface {
	ActiveDevices(ctx context.Context, userIDs []string) ([]string, error)
}

// InboxWriter batch-inserts inbox rows (one statement per batch, idempotent on
// the (device_id, msg_uuid) PK so redelivery is safe).
type InboxWriter interface {
	InsertBatch(ctx context.Context, rows []InboxRow) error
}

// DeliveryPublisher pushes a live delivery to a device (dev.{id}.out).
type DeliveryPublisher interface {
	PublishDelivery(ctx context.Context, deviceID string, row InboxRow) error
}

// AggregateNotifier records how many devices a message targets, so aggregate
// receipts can flip when all of them reach a state (§ receipts). Optional.
type AggregateNotifier interface {
	SetExpected(ctx context.Context, msgUUID string, expected int) error
}

// Worker performs one fan-out.
type Worker struct {
	members MemberResolver
	devices DeviceResolver
	inbox   InboxWriter
	pub     DeliveryPublisher
	agg     AggregateNotifier // may be nil
	log     *slog.Logger
}

func NewWorker(members MemberResolver, devices DeviceResolver, inbox InboxWriter, pub DeliveryPublisher, agg AggregateNotifier, log *slog.Logger) *Worker {
	return &Worker{members: members, devices: devices, inbox: inbox, pub: pub, agg: agg, log: log}
}

// Fanout expands, batch-inserts, and publishes. It returns the number of
// recipient device rows written. An insert error is returned (the job
// redelivers, dedupe absorbs the retry); a publish error is best-effort (inbox
// replay covers it).
func (w *Worker) Fanout(ctx context.Context, job FanoutJob) (int, error) {
	members, _, err := w.members.Members(ctx, job.GroupID, job.Version)
	if err != nil {
		return 0, err
	}
	devices, err := w.devices.ActiveDevices(ctx, members)
	if err != nil {
		return 0, err
	}

	rows := make([]InboxRow, 0, len(devices))
	for _, d := range devices {
		if d == job.SenderDeviceID {
			continue // the sending device already has its own message
		}
		rows = append(rows, InboxRow{
			DeviceID:       d,
			ConversationID: job.ConversationID,
			Seq:            job.Seq,
			MsgUUID:        job.MsgUUID,
			SenderUserID:   job.SenderUserID,
			SenderDeviceID: job.SenderDeviceID,
			Kind:           job.Kind,
			Ciphertext:     job.Ciphertext,
			AcceptedAtMS:   job.AcceptedAtMS,
			ExpiresAtMS:    job.ExpiresAtMS,
		})
	}

	if w.agg != nil && len(rows) > 0 {
		if err := w.agg.SetExpected(ctx, job.MsgUUID, len(rows)); err != nil {
			w.log.Warn("recording aggregate-receipt expectation failed", "msg_uuid", job.MsgUUID, "err", err)
		}
	}

	for start := 0; start < len(rows); start += BatchSize {
		end := start + BatchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[start:end]
		if err := w.inbox.InsertBatch(ctx, batch); err != nil {
			return start, err // rows before this batch are committed; retry re-covers via dedupe
		}
		// Publish as the batch commits (live subset gets it now; rest by replay).
		for i := range batch {
			if err := w.pub.PublishDelivery(ctx, batch[i].DeviceID, batch[i]); err != nil {
				w.log.Warn("group delivery publish failed (inbox covers it)", "device_id", batch[i].DeviceID, "err", err)
			}
		}
	}
	return len(rows), nil
}
