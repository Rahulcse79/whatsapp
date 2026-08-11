package fanout

import "context"

// AggregateStore tracks, per fanned message, how many target devices have
// reached a receipt state — so the sender sees ONE aggregate tick that flips
// only when ALL member devices reach it (messaging-groups-api.md: aggregate
// receipts; per-member detail is client-side). Backed by Valkey in production
// (a per-(msg,kind) set for dedupe + the expected total).
type AggregateStore interface {
	SetExpected(ctx context.Context, msgUUID string, expected int) error
	// Reached records deviceID reaching `kind` (delivered|read) for msgUUID and
	// returns the deduped device count, the expected total, and whether this
	// call added a new device (false for a duplicate receipt).
	Reached(ctx context.Context, msgUUID string, kind int16, deviceID string) (count, expected int, added bool, err error)
}

// AggregateEmitter delivers the aggregate tick to the sender's own devices.
type AggregateEmitter interface {
	EmitAggregate(ctx context.Context, senderUserID, msgUUID string, kind int16) error
}

// AggregateReceipts flips a group's aggregate delivered/read tick exactly once —
// when the last member device reaches the state.
type AggregateReceipts struct {
	store AggregateStore
	emit  AggregateEmitter
}

func NewAggregateReceipts(store AggregateStore, emit AggregateEmitter) *AggregateReceipts {
	return &AggregateReceipts{store: store, emit: emit}
}

// OnDeviceReceipt is called for each per-device receipt. It emits the aggregate
// tick once: on the call that newly brings the deduped count up to the expected
// total. Duplicate receipts (added=false) never re-emit.
func (a *AggregateReceipts) OnDeviceReceipt(ctx context.Context, senderUserID, msgUUID string, kind int16, deviceID string) error {
	count, expected, added, err := a.store.Reached(ctx, msgUUID, kind, deviceID)
	if err != nil {
		return err
	}
	if added && expected > 0 && count == expected {
		return a.emit.EmitAggregate(ctx, senderUserID, msgUUID, kind)
	}
	return nil
}
