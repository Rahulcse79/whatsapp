package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/whatsapp-v2/server/internal/auth"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

const (
	kindRead      = wsv1.ReceiptKind_RECEIPT_KIND_READ
	kindDelivered = wsv1.ReceiptKind_RECEIPT_KIND_DELIVERED
)

// newReceiptServer builds a server whose receipt source (dev.{id}.receipt) is
// the given DeliverySource, so a test can Publish receipts into the client.
func newReceiptServer(t *testing.T, chat ChatClient, receipts DeliverySource) (*httptest.Server, *auth.TokenIssuer) {
	t.Helper()
	issuer, err := auth.NewEphemeralIssuer(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{
		Verifier:   issuer,
		Authorizer: AllowAll{},
		Routes:     NewMemoryRouteStore(),
		Delivery:   NewMemoryDeliverySource(),
		Receipts:   receipts,
		Chat:       chat,
		Resume:     NewMemoryResumeStore(),
		PodID:      "pod-test",
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", s.Handle)
	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)
	return hs, issuer
}

// The coalescing guarantee (exact form): N receipts collapse to one entry per
// (conversation, kind) carrying the max seq.
func TestReceiptCoalescer_CollapsesToMaxSeq(t *testing.T) {
	c := newReceiptCoalescer()
	for _, seq := range []int64{3, 1, 9, 4} {
		c.submit("c1", kindRead, seq)
	}
	c.submit("c1", kindDelivered, 2) // separate kind
	c.submit("c2", kindRead, 7)      // separate conversation

	out := c.drain()
	if len(out) != 3 {
		t.Fatalf("drained %d entries, want 3: %v", len(out), out)
	}
	seqs := map[string]int64{}
	for _, r := range out {
		seqs[r.GetConversationId()+"/"+r.GetKind().String()] = r.GetUpToSeq()
	}
	if seqs["c1/RECEIPT_KIND_READ"] != 9 || seqs["c1/RECEIPT_KIND_DELIVERED"] != 2 || seqs["c2/RECEIPT_KIND_READ"] != 7 {
		t.Fatalf("wrong coalesced seqs: %v", seqs)
	}
	if len(c.drain()) != 0 {
		t.Fatal("drain must clear the buffer")
	}
}

func TestReceiptCoalescer_DropsJunk(t *testing.T) {
	c := newReceiptCoalescer()
	c.submit("", kindRead, 5)                                    // no conversation
	c.submit("c1", kindRead, 0)                                  // non-positive seq
	c.submit("c1", wsv1.ReceiptKind_RECEIPT_KIND_UNSPECIFIED, 3) // bad kind
	if out := c.drain(); len(out) != 0 {
		t.Fatalf("junk receipts survived: %v", out)
	}
}

// Inbound end-to-end: a burst of Receipt frames reaches core-api as ONE
// coalesced SubmitReceipt with the highest seq (N in a window ⇒ 1 rpc).
func TestReceipts_InboundCoalescedToCore(t *testing.T) {
	chat := &fakeChat{}
	hs, issuer := newResumeServer(t, chat, NewMemoryResumeStore())
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)
	readReplay(t, c)

	// Burst: 5 read receipts for the same conversation, ascending seq.
	for seq := int64(1); seq <= 5; seq++ {
		writeFrame(t, c, &wsv1.Frame{Body: &wsv1.Frame_Receipt{Receipt: &wsv1.Receipt{
			ConversationId: "c1", Kind: kindRead, UpToSeq: seq,
		}}})
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if rs := chat.submittedReceipts(); len(rs) > 0 {
			// The burst may straddle one flush boundary, but must NOT arrive
			// as 5 rpcs, and the final watermark must be the max seq.
			if len(rs) > 2 {
				t.Fatalf("burst reached core as %d rpcs — coalescing broken", len(rs))
			}
			last := rs[len(rs)-1]
			if last.GetUpToSeq() != 5 || last.GetConversationId() != "c1" || last.GetKind() != kindRead {
				// Straddling: wait for the second flush to carry seq 5.
				if time.Now().Before(deadline) {
					time.Sleep(20 * time.Millisecond)
					continue
				}
				t.Fatalf("final watermark wrong: %+v", last)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no receipt ever reached core-api")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// Outbound end-to-end: a receipt relayed on dev.{id}.receipt reaches the
// client as a Receipt frame with from_user_id intact.
func TestReceipts_OutboundForwarded(t *testing.T) {
	source := NewMemoryDeliverySource() // the receipt source
	hs, issuer := newReceiptServer(t, &fakeChat{}, source)

	tok, _ := issuer.Issue("u1", "r1", "sess1")
	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)
	readReplay(t, c)

	payload, _ := proto.Marshal(&wsv1.Receipt{
		ConversationId: "c9", Kind: kindDelivered, UpToSeq: 4, FromUserId: "peer-1",
	})
	source.Publish("r1", payload)

	deadline := time.Now().Add(3 * time.Second)
	for {
		f := readServerFrame(t, c)
		if rc := f.GetReceipt(); rc != nil {
			if rc.GetConversationId() != "c9" || rc.GetUpToSeq() != 4 || rc.GetFromUserId() != "peer-1" {
				t.Fatalf("forwarded receipt wrong: %+v", rc)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("receipt frame never arrived")
		}
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}
