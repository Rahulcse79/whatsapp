package bots

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/bots/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

const maxBotsPerOwner = 20

// Service runs the bot registry + the HMAC-signed webhook dispatch.
type Service struct {
	store      Store
	dispatcher Dispatcher
	now        func() time.Time
	newID      func() string
	newSecret  func() string
}

func NewService(store Store, dispatcher Dispatcher) *Service {
	return &Service{store: store, dispatcher: dispatcher, now: time.Now, newID: id.New, newSecret: randomSecret}
}

func randomSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RegisterBot creates a bot owned by the caller and returns its one-time secret.
func (s *Service) RegisterBot(ctx context.Context, ident auth.Identity, handle, name, webhookURL string) (RegisterResult, error) {
	handle = strings.ToLower(strings.TrimSpace(handle))
	if err := domain.ValidateHandle(handle); err != nil {
		return RegisterResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_HANDLE", err.Error())
	}
	if err := domain.ValidateName(name); err != nil {
		return RegisterResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_NAME", err.Error())
	}
	if err := domain.ValidateWebhookURL(webhookURL); err != nil {
		return RegisterResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_WEBHOOK", err.Error())
	}
	if _, err := s.store.GetByHandle(ctx, handle); err == nil {
		return RegisterResult{}, httpx.Reject(http.StatusConflict, "HANDLE_TAKEN", ErrHandleTaken.Error())
	} else if !errors.Is(err, ErrNotFound) {
		return RegisterResult{}, httpx.Transient()
	}
	existing, err := s.store.ListByOwner(ctx, ident.UserID)
	if err != nil {
		return RegisterResult{}, httpx.Transient()
	}
	if len(existing) >= maxBotsPerOwner {
		return RegisterResult{}, httpx.Reject(http.StatusConflict, "STATE_TOO_MANY_BOTS", "too many bots; delete one first")
	}
	b := Bot{ID: s.newID(), OwnerID: ident.UserID, Handle: handle, Name: strings.TrimSpace(name), WebhookURL: webhookURL, Secret: s.newSecret(), CreatedAt: s.now()}
	if err := s.store.Create(ctx, b); err != nil {
		return RegisterResult{}, httpx.Transient()
	}
	return RegisterResult{Bot: view(b), Secret: b.Secret}, nil
}

// ListBots returns the caller's bots (no secrets).
func (s *Service) ListBots(ctx context.Context, ident auth.Identity) ([]BotView, error) {
	bs, err := s.store.ListByOwner(ctx, ident.UserID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]BotView, len(bs))
	for i, b := range bs {
		out[i] = view(b)
	}
	return out, nil
}

// DeleteBot removes one of the caller's bots.
func (s *Service) DeleteBot(ctx context.Context, ident auth.Identity, botID string) error {
	if err := s.store.Delete(ctx, ident.UserID, botID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// RotateSecret issues a fresh secret for one of the caller's bots.
func (s *Service) RotateSecret(ctx context.Context, ident auth.Identity, botID string) (string, error) {
	b, err := s.load(ctx, botID)
	if err != nil {
		return "", err
	}
	if b.OwnerID != ident.UserID {
		return "", notFound()
	}
	secret := s.newSecret()
	if err := s.store.SetSecret(ctx, botID, secret); err != nil {
		return "", httpx.Transient()
	}
	return secret, nil
}

// DispatchEvent signs an event with the bot's secret and delivers it to the bot's
// webhook. Used by the message-routing integration when a user interacts with a
// bot (the routing trigger itself is a seam). Returns an error only when the bot
// can't be resolved; delivery failures are the dispatcher's to log/retry.
func (s *Service) DispatchEvent(ctx context.Context, botID string, ev Event) error {
	b, err := s.load(ctx, botID)
	if err != nil {
		return err
	}
	ev.BotID = botID
	if ev.AtMS == 0 {
		ev.AtMS = s.now().UnixMilli()
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return httpx.Transient()
	}
	sig := domain.Sign([]byte(b.Secret), payload)
	return s.dispatcher.Deliver(ctx, b.WebhookURL, sig, payload)
}

func (s *Service) load(ctx context.Context, botID string) (Bot, error) {
	b, err := s.store.Get(ctx, botID)
	if errors.Is(err, ErrNotFound) {
		return Bot{}, notFound()
	}
	if err != nil {
		return Bot{}, httpx.Transient()
	}
	return b, nil
}

func view(b Bot) BotView {
	return BotView{ID: b.ID, Handle: b.Handle, Name: b.Name, WebhookURL: b.WebhookURL, CreatedAtMS: b.CreatedAt.UnixMilli()}
}

func notFound() error {
	return httpx.Reject(http.StatusNotFound, "BOT_NOT_FOUND", "bot not found")
}
