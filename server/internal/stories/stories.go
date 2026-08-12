package stories

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by the store when no story matches.
var ErrNotFound = errors.New("stories: not found")

// Story is a stories row. MediaRef is the media_objects id of the ciphertext
// blob (nil for a text story). Audience is the eligible-viewer set frozen at
// post time. Kind is not persisted (see FeedItem) — it's a post-time input only.
type Story struct {
	ID        string
	AuthorID  string
	MediaRef  *string
	Audience  []string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// PostResult is the POST /stories response.
type PostResult struct {
	StoryID   string `json:"story_id"`
	ExpiresAt int64  `json:"expires_at_ms"`
}

// FeedItem is one GET /feed entry (media-stories-api.md: story_id, author,
// expires_at, key_available). Kind is a post-time input only — it isn't stored
// or returned (the client derives content type from the E2EE payload).
type FeedItem struct {
	StoryID   string `json:"story_id"`
	Author    string `json:"author"`
	ExpiresAt int64  `json:"expires_at_ms"`
	// KeyAvailable: the story carries media a per-story key applies to (the
	// client tracks actual STORY_KEY receipt — the server never holds the key).
	KeyAvailable bool `json:"key_available"`
}

// Viewer is one GET /viewers entry (author-only).
type Viewer struct {
	UserID   string `json:"user_id"`
	ViewedAt int64  `json:"viewed_at_ms"`
}

// Store persists stories + story_views.
type Store interface {
	Create(ctx context.Context, s Story) error
	Get(ctx context.Context, id string) (Story, error) // ErrNotFound
	// Feed returns stories whose audience includes viewerID and that have not
	// expired at now.
	Feed(ctx context.Context, viewerID string, now time.Time) ([]Story, error)
	RecordView(ctx context.Context, storyID, viewerID string) error // idempotent
	Viewers(ctx context.Context, storyID string) ([]Viewer, error)
	Delete(ctx context.Context, id string) error
	// PurgeExpired hard-deletes stories past their expiry (views cascade).
	PurgeExpired(ctx context.Context, now time.Time) (int, error)
}

// Audience resolves an author's default story audience — their contacts.
type Audience interface {
	ContactsOf(ctx context.Context, authorID string) ([]string, error)
}
