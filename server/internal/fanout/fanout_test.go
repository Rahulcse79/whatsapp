package fanout

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeMembers struct {
	members   []string
	gotMinVer int64
}

func (f *fakeMembers) Members(_ context.Context, _ string, minVer int64) ([]string, int64, error) {
	f.gotMinVer = minVer
	return f.members, minVer, nil
}

type fakeDevices struct{ devices []string }

func (f *fakeDevices) ActiveDevices(_ context.Context, _ []string) ([]string, error) {
	return f.devices, nil
}

type fakeInbox struct {
	batchSizes []int
	rows       []InboxRow
	failAt     int
	calls      int
}

func (f *fakeInbox) InsertBatch(_ context.Context, rows []InboxRow) error {
	f.calls++
	if f.failAt > 0 && f.calls == f.failAt {
		return errors.New("insert failed")
	}
	f.batchSizes = append(f.batchSizes, len(rows))
	f.rows = append(f.rows, rows...)
	return nil
}

type fakePub struct{ devices []string }

func (f *fakePub) PublishDelivery(_ context.Context, deviceID string, _ InboxRow) error {
	f.devices = append(f.devices, deviceID)
	return nil
}

type fakeAgg struct{ expected int }

func (f *fakeAgg) SetExpected(_ context.Context, _ string, expected int) error {
	f.expected = expected
	return nil
}

func TestFanout_ExpandsBatchesAndPublishes(t *testing.T) {
	// 502 devices (incl. the sender's own) → 501 rows → batches of 500 + 1.
	devs := []string{"sender-dev"}
	for i := 0; i < 501; i++ {
		devs = append(devs, fmt.Sprintf("d%03d", i))
	}
	members := &fakeMembers{members: []string{"u1", "u2"}}
	inbox := &fakeInbox{}
	pub := &fakePub{}
	agg := &fakeAgg{}
	w := NewWorker(members, &fakeDevices{devices: devs}, inbox, pub, agg, testLog())

	n, err := w.Fanout(context.Background(), FanoutJob{
		GroupID: "g1", ConversationID: "g1", Seq: 5, MsgUUID: "m1",
		SenderUserID: "u1", SenderDeviceID: "sender-dev", Kind: 1, Ciphertext: []byte("ct"), Version: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 501 {
		t.Fatalf("rows = %d, want 501 (502 devices minus the sender's own)", n)
	}
	if got := inbox.batchSizes; len(got) != 2 || got[0] != 500 || got[1] != 1 {
		t.Fatalf("batch sizes = %v, want [500 1]", got)
	}
	if len(pub.devices) != 501 {
		t.Fatalf("published %d deliveries, want 501", len(pub.devices))
	}
	for _, d := range pub.devices {
		if d == "sender-dev" {
			t.Fatal("must not deliver to the sending device")
		}
	}
	if agg.expected != 501 {
		t.Fatalf("aggregate expected = %d, want 501", agg.expected)
	}
	if members.gotMinVer != 3 {
		t.Fatalf("membership resolved at minVersion %d, want 3 (the accept version)", members.gotMinVer)
	}
}

func TestFanout_InsertErrorReturns(t *testing.T) {
	w := NewWorker(
		&fakeMembers{members: []string{"u1"}},
		&fakeDevices{devices: []string{"d1", "d2"}},
		&fakeInbox{failAt: 1},
		&fakePub{},
		nil,
		testLog(),
	)
	if _, err := w.Fanout(context.Background(), FanoutJob{GroupID: "g1", MsgUUID: "m1", SenderDeviceID: "x"}); err == nil {
		t.Fatal("insert error must propagate so the job redelivers")
	}
}

func TestFanout_NilAggregateIsOptional(t *testing.T) {
	w := NewWorker(
		&fakeMembers{members: []string{"u1"}},
		&fakeDevices{devices: []string{"d1"}},
		&fakeInbox{},
		&fakePub{},
		nil, // no aggregate notifier
		testLog(),
	)
	if n, err := w.Fanout(context.Background(), FanoutJob{GroupID: "g1", MsgUUID: "m1", SenderDeviceID: "x"}); err != nil || n != 1 {
		t.Fatalf("fanout without aggregate notifier failed: n=%d err=%v", n, err)
	}
}
