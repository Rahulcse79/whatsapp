package chat

import (
	"sort"
	"sync"
	"testing"
)

// The coalescing guarantee: many receipts for one conversation within a
// window collapse to a single entry carrying the highest seq.
func TestReceiptCoalescer_CollapsesToOne(t *testing.T) {
	c := NewReceiptCoalescer()
	for i := int64(1); i <= 20; i++ {
		c.Submit("c1", ReceiptRead, i)
	}
	out := c.Drain()
	if len(out) != 1 {
		t.Fatalf("20 receipts collapsed to %d entries, want 1", len(out))
	}
	if out[0].UpToSeq != 20 || out[0].Kind != ReceiptRead || out[0].ConversationID != "c1" {
		t.Fatalf("coalesced entry wrong: %+v", out[0])
	}
}

// Out-of-order arrivals still keep the maximum seq (cumulative watermark).
func TestReceiptCoalescer_KeepsMaxSeq(t *testing.T) {
	c := NewReceiptCoalescer()
	for _, seq := range []int64{5, 2, 9, 1, 7} {
		c.Submit("c1", ReceiptDelivered, seq)
	}
	out := c.Drain()
	if len(out) != 1 || out[0].UpToSeq != 9 {
		t.Fatalf("want single entry with seq 9, got %+v", out)
	}
}

// Different conversations and kinds are coalesced independently.
func TestReceiptCoalescer_SeparateKeys(t *testing.T) {
	c := NewReceiptCoalescer()
	c.Submit("c1", ReceiptDelivered, 3)
	c.Submit("c1", ReceiptRead, 2)
	c.Submit("c2", ReceiptRead, 5)
	out := c.Drain()
	if len(out) != 3 {
		t.Fatalf("want 3 distinct (conv,kind) entries, got %d: %+v", len(out), out)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpToSeq < out[j].UpToSeq })
	if out[0].UpToSeq != 2 || out[1].UpToSeq != 3 || out[2].UpToSeq != 5 {
		t.Fatalf("unexpected coalesced set: %+v", out)
	}
}

// Drain clears the buffer: a second drain with no new receipts is empty.
func TestReceiptCoalescer_DrainClears(t *testing.T) {
	c := NewReceiptCoalescer()
	c.Submit("c1", ReceiptRead, 1)
	if len(c.Drain()) != 1 {
		t.Fatal("first drain should return the receipt")
	}
	if len(c.Drain()) != 0 {
		t.Fatal("second drain should be empty")
	}
}

func TestReceiptCoalescer_ConcurrentSubmit(t *testing.T) {
	c := NewReceiptCoalescer()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(seq int64) {
			defer wg.Done()
			c.Submit("c1", ReceiptRead, seq)
		}(int64(i + 1))
	}
	wg.Wait()
	out := c.Drain()
	if len(out) != 1 || out[0].UpToSeq != 50 {
		t.Fatalf("concurrent submit: want single entry seq 50, got %+v", out)
	}
}
