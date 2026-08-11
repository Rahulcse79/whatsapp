package fanout

import (
	"context"
	"testing"
	"time"
)

func TestPool_SubmitBackpressure(t *testing.T) {
	p := NewPool(nil, 1, 2, testLog()) // not Started → the queue fills up
	if !p.Submit(FanoutJob{}) || !p.Submit(FanoutJob{}) {
		t.Fatal("first two submits should fit the queue")
	}
	if p.Submit(FanoutJob{}) {
		t.Fatal("third submit must be rejected (queue full → backpressure)")
	}
	if p.QueueDepth() != 2 {
		t.Fatalf("queue depth = %d, want 2", p.QueueDepth())
	}
}

type signalInbox struct{ done chan struct{} }

func (s *signalInbox) InsertBatch(_ context.Context, _ []InboxRow) error {
	s.done <- struct{}{}
	return nil
}

func TestPool_ProcessesSubmittedJobs(t *testing.T) {
	done := make(chan struct{}, 1)
	w := NewWorker(
		&fakeMembers{members: []string{"u1"}},
		&fakeDevices{devices: []string{"d1"}},
		&signalInbox{done: done},
		&fakePub{},
		nil,
		testLog(),
	)
	p := NewPool(w, 2, 8, testLog())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	if !p.Submit(FanoutJob{GroupID: "g1", MsgUUID: "m1", SenderDeviceID: "x"}) {
		t.Fatal("submit failed")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("submitted job was not processed by the pool")
	}
}
