package whiteboard

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

type fakeStore struct {
	members map[string]map[string]bool
	ops     []Op
}

func newFake() *fakeStore { return &fakeStore{members: map[string]map[string]bool{}} }
func (s *fakeStore) IsMember(_ context.Context, convID, userID string) (bool, error) {
	return s.members[convID][userID], nil
}
func (s *fakeStore) AppendOp(_ context.Context, o Op) error {
	for _, e := range s.ops {
		if e.ConversationID == o.ConversationID && e.ID == o.ID {
			return nil // idempotent
		}
	}
	s.ops = append(s.ops, o)
	return nil
}
func (s *fakeStore) ListOps(_ context.Context, convID string, since int64, _ int) ([]Op, error) {
	var out []Op
	for _, o := range s.ops {
		if o.ConversationID == convID && o.Seq > since {
			out = append(out, o)
		}
	}
	return out, nil
}
func (s *fakeStore) MaxSeq(_ context.Context, convID string) (int64, error) {
	var m int64
	for _, o := range s.ops {
		if o.ConversationID == convID && o.Seq > m {
			m = o.Seq
		}
	}
	return m, nil
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

func newSvc() (*Service, *fakeStore) {
	st := newFake()
	st.members["c1"] = map[string]bool{"alice": true, "bob": true}
	return NewService(st), st
}

func op(id, kind string, seq int64) Op {
	return Op{ID: id, Kind: kind, Seq: seq, Data: json.RawMessage([]byte(`{"t":"` + kind + `","id":"` + id + `"}`))}
}

func TestAppendAndSync(t *testing.T) {
	svc, _ := newSvc()
	// non-member can't touch the board
	if err := svc.Append(context.Background(), who("mallory"), "c1", []Op{op("s1", "stroke", 1)}); codeOf(t, err) != "BOARD_NOT_FOUND" {
		t.Fatal("non-member append should 404")
	}
	if err := svc.Append(context.Background(), who("alice"), "c1", []Op{op("s1", "stroke", 1), op("s2", "stroke", 2)}); err != nil {
		t.Fatalf("append: %v", err)
	}
	res, err := svc.Sync(context.Background(), who("bob"), "c1", 0)
	if err != nil || len(res.Ops) != 2 || res.Cursor != 2 {
		t.Fatalf("sync from 0: %v %+v", err, res)
	}
	// bob draws; alice syncs only what's new past her cursor
	if err := svc.Append(context.Background(), who("bob"), "c1", []Op{op("s3", "stroke", 3)}); err != nil {
		t.Fatalf("append s3: %v", err)
	}
	res2, _ := svc.Sync(context.Background(), who("alice"), "c1", 2)
	if len(res2.Ops) != 1 || res2.Cursor != 3 {
		t.Fatalf("incremental sync: %+v", res2)
	}
}

func TestAppendForcesAuthorAndValidates(t *testing.T) {
	svc, st := newSvc()
	if err := svc.Append(context.Background(), who("alice"), "c1", []Op{op("s1", "stroke", 1)}); err != nil {
		t.Fatal(err)
	}
	if st.ops[0].Author != "alice" {
		t.Fatalf("author should be forced to caller, got %q", st.ops[0].Author)
	}
	if err := svc.Append(context.Background(), who("alice"), "c1", nil); codeOf(t, err) != "VALIDATION_BATCH" {
		t.Fatal("empty batch should 400")
	}
	if err := svc.Append(context.Background(), who("alice"), "c1", []Op{op("s2", "scribble", 1)}); codeOf(t, err) != "VALIDATION_OP" {
		t.Fatal("bad kind should 400")
	}
}

func TestAppendIdempotent(t *testing.T) {
	svc, st := newSvc()
	_ = svc.Append(context.Background(), who("alice"), "c1", []Op{op("s1", "stroke", 1)})
	_ = svc.Append(context.Background(), who("alice"), "c1", []Op{op("s1", "stroke", 1)}) // retry
	if len(st.ops) != 1 {
		t.Fatalf("retry should not duplicate, got %d ops", len(st.ops))
	}
}
