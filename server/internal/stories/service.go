// Package stories owns status posts: audience snapshots frozen at post time, 24 h
// hard expiry, and view receipts. Content is E2E-encrypted with per-story keys
// distributed client-side (WS MsgSend{STORY_KEY}); this package handles the
// ciphertext ref + metadata only (media-stories-api.md §Stories, HLD §12).
package stories

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
	"github.com/whatsapp-v2/server/internal/stories/domain"
)

// Service orchestrates posting, the feed, view receipts, viewer lists, and the
// hard-expiry purge.
type Service struct {
	store    Store
	audience Audience
	now      func() time.Time
	newID    func() string
}

func NewService(store Store, audience Audience) *Service {
	return &Service{store: store, audience: audience, now: time.Now, newID: id.New}
}

// Post creates a story. The audience is the override if supplied, else the
// author's contacts — frozen now (later contact changes don't retro-affect who
// can see it). expires_at = now + 24 h.
func (s *Service) Post(ctx context.Context, ident auth.Identity, kindStr string, mediaRef *string, override []string) (PostResult, error) {
	kind, ok := domain.ParseKind(kindStr)
	if !ok {
		return PostResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_KIND", "kind must be image, video, or text")
	}

	var aud []string
	if override != nil {
		aud = domain.Audience(ident.UserID, override, nil)
	} else {
		contacts, err := s.audience.ContactsOf(ctx, ident.UserID)
		if err != nil {
			return PostResult{}, httpx.Transient()
		}
		aud = domain.Audience(ident.UserID, nil, contacts)
	}

	if err := domain.ValidateCreate(kind, mediaRef != nil, len(aud)); err != nil {
		return PostResult{}, validationError(err)
	}

	now := s.now()
	story := Story{
		ID: s.newID(), AuthorID: ident.UserID, MediaRef: mediaRef,
		Audience: aud, ExpiresAt: domain.ExpiryFrom(now), CreatedAt: now,
	}
	if err := s.store.Create(ctx, story); err != nil {
		return PostResult{}, httpx.Transient()
	}
	return PostResult{StoryID: story.ID, ExpiresAt: story.ExpiresAt.UnixMilli()}, nil
}

// Feed returns the caller's visible, unexpired stories.
func (s *Service) Feed(ctx context.Context, ident auth.Identity) ([]FeedItem, error) {
	list, err := s.store.Feed(ctx, ident.UserID, s.now())
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]FeedItem, 0, len(list))
	for _, st := range list {
		out = append(out, FeedItem{
			StoryID: st.ID, Author: st.AuthorID,
			ExpiresAt: st.ExpiresAt.UnixMilli(), KeyAvailable: st.MediaRef != nil,
		})
	}
	return out, nil
}

// View records a view receipt. Only a story in the caller's audience (and not
// expired) can be viewed; anything else is 404 (never confirm a story the caller
// may not see exists).
func (s *Service) View(ctx context.Context, ident auth.Identity, storyID string) error {
	st, err := s.load(ctx, storyID)
	if err != nil {
		return err
	}
	if s.now().After(st.ExpiresAt) || !contains(st.Audience, ident.UserID) {
		return notFound()
	}
	if err := s.store.RecordView(ctx, storyID, ident.UserID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Viewers lists who viewed a story — author only.
func (s *Service) Viewers(ctx context.Context, ident auth.Identity, storyID string) ([]Viewer, error) {
	st, err := s.load(ctx, storyID)
	if err != nil {
		return nil, err
	}
	if st.AuthorID != ident.UserID {
		return nil, notFound()
	}
	viewers, err := s.store.Viewers(ctx, storyID)
	if err != nil {
		return nil, httpx.Transient()
	}
	return viewers, nil
}

// Delete removes a story early — author only. The 24 h purge + MinIO ILM are the
// backstops regardless.
func (s *Service) Delete(ctx context.Context, ident auth.Identity, storyID string) error {
	st, err := s.load(ctx, storyID)
	if err != nil {
		return err
	}
	if st.AuthorID != ident.UserID {
		return notFound()
	}
	if err := s.store.Delete(ctx, storyID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// PurgeExpired hard-deletes stories past their expiry (the 24 h job).
func (s *Service) PurgeExpired(ctx context.Context) (int, error) {
	return s.store.PurgeExpired(ctx, s.now())
}

func (s *Service) load(ctx context.Context, storyID string) (Story, error) {
	st, err := s.store.Get(ctx, storyID)
	if errors.Is(err, ErrNotFound) {
		return Story{}, notFound()
	}
	if err != nil {
		return Story{}, httpx.Transient()
	}
	return st, nil
}

func notFound() error {
	return httpx.Reject(http.StatusNotFound, "STORY_NOT_FOUND", "no such story")
}

func validationError(err error) error {
	switch {
	case errors.Is(err, domain.ErrMediaMissing):
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_MEDIA", "image/video story needs a media ref")
	case errors.Is(err, domain.ErrMediaOnText):
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_MEDIA", "text story must not carry a media ref")
	case errors.Is(err, domain.ErrNoAudience):
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_AUDIENCE", "audience is empty")
	default:
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_KIND", "invalid story")
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
