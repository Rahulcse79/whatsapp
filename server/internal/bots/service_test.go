package bots

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/bots/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

type fakeStore struct{ bots map[string]Bot }

func newFake() *fakeStore                                  { return &fakeStore{bots: map[string]Bot{}} }
func (s *fakeStore) Create(_ context.Context, b Bot) error { s.bots[b.ID] = b; return nil }
func (s *fakeStore) Get(_ context.Context, id string) (Bot, error) {
	b, ok := s.bots[id]
	if !ok {
		return Bot{}, ErrNotFound
	}
	return b, nil
}
func (s *fakeStore) GetByHandle(_ context.Context, handle string) (Bot, error) {
	for _, b := range s.bots {
		if b.Handle == handle {
			return b, nil
		}
	}
	return Bot{}, ErrNotFound
}
func (s *fakeStore) ListByOwner(_ context.Context, ownerID string) ([]Bot, error) {
	var out []Bot
	for _, b := range s.bots {
		if b.OwnerID == ownerID {
			out = append(out, b)
		}
	}
	return out, nil
}
func (s *fakeStore) Delete(_ context.Context, ownerID, id string) error {
	if b, ok := s.bots[id]; ok && b.OwnerID == ownerID {
		delete(s.bots, id)
	}
	return nil
}
func (s *fakeStore) SetSecret(_ context.Context, id, secret string) error {
	b := s.bots[id]
	b.Secret = secret
	s.bots[id] = b
	return nil
}

type fakeDispatcher struct {
	url     string
	sig     string
	payload []byte
}

func (d *fakeDispatcher) Deliver(_ context.Context, url, signature string, payload []byte) error {
	d.url, d.sig, d.payload = url, signature, payload
	return nil
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

func newSvc() (*Service, *fakeDispatcher) {
	disp := &fakeDispatcher{}
	svc := NewService(newFake(), disp)
	n, sn := 0, 0
	svc.newID = func() string { n++; return fmt.Sprintf("bot%d", n) }
	svc.newSecret = func() string { sn++; return fmt.Sprintf("secret%d", sn) }
	svc.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return svc, disp
}

func TestRegisterAndList(t *testing.T) {
	svc, _ := newSvc()
	res, err := svc.RegisterBot(context.Background(), who("alice"), "News_Bot", "News", "https://bot.example/hook")
	if err != nil || res.Secret == "" || res.Bot.Handle != "news_bot" {
		t.Fatalf("register: %v %+v", err, res)
	}
	// duplicate handle → 409
	if _, err := svc.RegisterBot(context.Background(), who("bob"), "news_bot", "Dup", "https://x.example/h"); codeOf(t, err) != "HANDLE_TAKEN" {
		t.Fatal("duplicate handle should 409")
	}
	// bad webhook → 400
	if _, err := svc.RegisterBot(context.Background(), who("alice"), "other_bot", "Other", "http://insecure"); codeOf(t, err) != "VALIDATION_WEBHOOK" {
		t.Fatal("non-https webhook should 400")
	}
	list, _ := svc.ListBots(context.Background(), who("alice"))
	if len(list) != 1 || list[0].Handle != "news_bot" {
		t.Fatalf("list: %+v", list)
	}
}

func TestRotateSecretOwnerGate(t *testing.T) {
	svc, _ := newSvc()
	res, _ := svc.RegisterBot(context.Background(), who("alice"), "abc_bot", "ABC", "https://b.example/h")
	// non-owner can't rotate
	if _, err := svc.RotateSecret(context.Background(), who("mallory"), res.Bot.ID); codeOf(t, err) != "BOT_NOT_FOUND" {
		t.Fatal("non-owner rotate should 404")
	}
	secret, err := svc.RotateSecret(context.Background(), who("alice"), res.Bot.ID)
	if err != nil || secret == res.Secret {
		t.Fatalf("rotate should produce a new secret: %v %q", err, secret)
	}
}

func TestDispatchEventSignsWithBotSecret(t *testing.T) {
	svc, disp := newSvc()
	res, _ := svc.RegisterBot(context.Background(), who("alice"), "sig_bot", "Sig", "https://bot.example/hook")
	if err := svc.DispatchEvent(context.Background(), res.Bot.ID, Event{Type: "message", Text: "hello", UserID: "u"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if disp.url != "https://bot.example/hook" {
		t.Fatalf("delivered to wrong url: %s", disp.url)
	}
	// the signature must verify under the bot's secret
	if !domain.VerifySignature([]byte(res.Secret), disp.payload, disp.sig) {
		t.Fatal("event signature does not verify with the bot secret")
	}
	// a wrong secret must NOT verify
	if domain.VerifySignature([]byte("nope"), disp.payload, disp.sig) {
		t.Fatal("wrong secret should not verify")
	}
}
