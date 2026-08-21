package notifyprefs

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/notifyprefs/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

const (
	maxScheduledPerUser = 100
	maxTitleLen         = 200
)

// Service is the notification-preferences control plane.
type Service struct {
	store Store
	now   func() time.Time
	newID func() string
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now, newID: id.New}
}

// GetPrefs returns the caller's prefs, defaulting when none are stored.
func (s *Service) GetPrefs(ctx context.Context, ident auth.Identity) (domain.Prefs, error) {
	p, err := s.store.GetPrefs(ctx, ident.UserID)
	if errors.Is(err, ErrNotFound) {
		return domain.DefaultPrefs(), nil
	}
	if err != nil {
		return domain.Prefs{}, httpx.Transient()
	}
	return p, nil
}

// SetPrefs validates and persists the caller's prefs.
func (s *Service) SetPrefs(ctx context.Context, ident auth.Identity, p domain.Prefs) error {
	if err := domain.Validate(p); err != nil {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_PREFS", err.Error())
	}
	if err := s.store.UpsertPrefs(ctx, ident.UserID, p); err != nil {
		return httpx.Transient()
	}
	return nil
}

// SetSnooze snoozes a conversation until `until`; a non-future time clears it.
func (s *Service) SetSnooze(ctx context.Context, ident auth.Identity, conversationID string, until time.Time) error {
	if strings.TrimSpace(conversationID) == "" {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_CONVERSATION", "conversation id is required")
	}
	if !until.After(s.now()) {
		if err := s.store.ClearSnooze(ctx, ident.UserID, conversationID); err != nil {
			return httpx.Transient()
		}
		return nil
	}
	if err := s.store.SetSnooze(ctx, ident.UserID, conversationID, until); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ClearSnooze un-snoozes a conversation.
func (s *Service) ClearSnooze(ctx context.Context, ident auth.Identity, conversationID string) error {
	if err := s.store.ClearSnooze(ctx, ident.UserID, conversationID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ScheduleNotification creates a content-free reminder for the caller.
func (s *Service) ScheduleNotification(ctx context.Context, ident auth.Identity, conversationID, title string, dueAt time.Time) (ScheduledView, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > maxTitleLen {
		return ScheduledView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_TITLE", "title is required (max 200 chars)")
	}
	if !dueAt.After(s.now()) {
		return ScheduledView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_DUE", "due time must be in the future")
	}
	existing, err := s.store.ListScheduled(ctx, ident.UserID)
	if err != nil {
		return ScheduledView{}, httpx.Transient()
	}
	if len(existing) >= maxScheduledPerUser {
		return ScheduledView{}, httpx.Reject(http.StatusConflict, "STATE_TOO_MANY_SCHEDULED", "too many scheduled reminders; cancel one first")
	}
	n := ScheduledNotification{ID: s.newID(), UserID: ident.UserID, ConversationID: strings.TrimSpace(conversationID), Title: title, DueAt: dueAt, CreatedAt: s.now()}
	if err := s.store.CreateScheduled(ctx, n); err != nil {
		return ScheduledView{}, httpx.Transient()
	}
	return viewScheduled(n), nil
}

// ListScheduled returns the caller's reminders (newest-due last is the store's
// order; the handler returns them as-is).
func (s *Service) ListScheduled(ctx context.Context, ident auth.Identity) ([]ScheduledView, error) {
	ns, err := s.store.ListScheduled(ctx, ident.UserID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]ScheduledView, len(ns))
	for i, n := range ns {
		out[i] = viewScheduled(n)
	}
	return out, nil
}

// CancelScheduled deletes one of the caller's reminders.
func (s *Service) CancelScheduled(ctx context.Context, ident auth.Identity, notifID string) error {
	if err := s.store.DeleteScheduled(ctx, ident.UserID, notifID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// FireDue marks every reminder due at or before now as fired and returns them —
// the due-scan step a background ticker drives (the wake-emission that turns a
// fired reminder into an actual push/nudge is the documented pipeline seam).
func (s *Service) FireDue(ctx context.Context, limit int) ([]ScheduledNotification, error) {
	if limit <= 0 {
		limit = 100
	}
	due, err := s.store.DueBefore(ctx, s.now(), limit)
	if err != nil {
		return nil, err
	}
	fired := s.now()
	out := make([]ScheduledNotification, 0, len(due))
	for _, n := range due {
		if err := s.store.MarkFired(ctx, n.ID, fired); err != nil {
			return out, err // partial progress is fine; the next tick retries the rest
		}
		n.FiredAt = &fired
		out = append(out, n)
	}
	return out, nil
}

func viewScheduled(n ScheduledNotification) ScheduledView {
	return ScheduledView{ID: n.ID, ConversationID: n.ConversationID, Title: n.Title, DueAtMS: n.DueAt.UnixMilli(), Fired: n.FiredAt != nil}
}
