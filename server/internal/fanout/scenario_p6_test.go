package fanout

import (
	"context"
	"fmt"
	"testing"
)

// TestScenarioP6 is the server half of protocol scenario P6 (test-strategy.md §3):
// a 1,024-member group send fans out to every member device except the sender's
// own, batched, and the aggregate delivered/read tick flips EXACTLY once when the
// last of those devices reaches the state. The membership is resolved at the
// accept version, so a cache lagging a membership change (e.g. the key-rotation
// remove) is bypassed — the post-change member set is used. (The client half —
// Sender-Key rotation ordering — is crypto-wrapper senderKey.p6.test.ts.)
func TestScenarioP6_GroupFanout1024AndAggregateReceipts(t *testing.T) {
	const members = 1024
	// One device per member, including the sender's own device.
	devs := []string{"dev-alice"}
	memberIDs := []string{"alice"}
	for i := 0; i < members-1; i++ {
		devs = append(devs, fmt.Sprintf("dev%04d", i))
		memberIDs = append(memberIDs, fmt.Sprintf("u%04d", i))
	}

	inbox := &fakeInbox{}
	pub := &fakePub{}
	agg := &fakeAgg{}
	mem := &fakeMembers{members: memberIDs}
	w := NewWorker(mem, &fakeDevices{devices: devs}, inbox, pub, agg, testLog())

	const acceptVersion = 42
	n, err := w.Fanout(context.Background(), FanoutJob{
		GroupID: "g", ConversationID: "g", Seq: 7, MsgUUID: "m1",
		SenderUserID: "alice", SenderDeviceID: "dev-alice", Kind: 1, Ciphertext: []byte("ct"), Version: acceptVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1023 {
		t.Fatalf("rows = %d, want 1023 (1024 devices minus the sender's own)", n)
	}
	// 1023 rows → 500 + 500 + 23.
	if got := inbox.batchSizes; len(got) != 3 || got[0] != 500 || got[1] != 500 || got[2] != 23 {
		t.Fatalf("batch sizes = %v, want [500 500 23]", got)
	}
	if len(pub.devices) != 1023 {
		t.Fatalf("published %d deliveries, want 1023", len(pub.devices))
	}
	for _, d := range pub.devices {
		if d == "dev-alice" {
			t.Fatal("must not deliver to the sending device")
		}
	}
	if agg.expected != 1023 {
		t.Fatalf("aggregate expected = %d, want 1023", agg.expected)
	}
	// Rotation ordering: membership resolved at the accept version, so a cache
	// lagging the membership change is bypassed (data-structures §12).
	if mem.gotMinVer != acceptVersion {
		t.Fatalf("membership resolved at minVersion %d, want %d (accept version)", mem.gotMinVer, acceptVersion)
	}

	// Aggregate receipts flip EXACTLY once when the last of the 1,023 devices
	// reaches the state — no receipt storm, no early flip.
	store, emit := newMemAgg(), &recEmit{}
	ar := NewAggregateReceipts(store, emit)
	ctx := context.Background()
	_ = store.SetExpected(ctx, "m1", 1023)
	for i := 0; i < 1023; i++ {
		if err := ar.OnDeviceReceipt(ctx, "alice", "m1", 1, fmt.Sprintf("dev%04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if emit.emits != 1 {
		t.Fatalf("aggregate tick emits = %d, want exactly 1 after all 1023 devices", emit.emits)
	}
	// A late duplicate from any device must not re-flip the aggregate.
	_ = ar.OnDeviceReceipt(ctx, "alice", "m1", 1, "dev0000")
	if emit.emits != 1 {
		t.Fatalf("duplicate receipt re-emitted the aggregate tick: %d", emit.emits)
	}
}
