package stories

import (
	"context"
	"errors"
	"time"

	"github.com/whatsapp-v2/server/internal/stories/domain"
)

// ErrNotFound is returned by the store when no story matches.
var ErrNotFound = errors.New("stories: not found")

// Story is a stories row. MediaRef is the object key of the ciphertext blob
// (nil for a text story). Audience is the eligible-viewer set frozen at post
// time.
//
// Kind IS persisted (migration 000032). It used to be a post-time input only,
// which left a viewer unable to tell what a story even was — see FeedItem.
type Story struct {
	ID        string
	AuthorID  string
	Kind      domain.Kind
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

// FeedItem is one GET /feed entry.
//
// It now carries `kind` and `media_ref`, without which a viewer could not
// render a story at all: it had no idea whether the story was text or video,
// and no way to locate the ciphertext. Returning the object key to an audience
// member leaks nothing — the blob is encrypted under a per-story key the server
// never sees, and the feed is already restricted to the audience snapshot.
type FeedItem struct {
	StoryID string `json:"story_id"`
	Author  string `json:"author"`
	// Kind is "image" | "video" | "text".
	Kind string `json:"kind"`
	// MediaRef is the ciphertext object key, empty for a text story.
	MediaRef  string `json:"media_ref,omitempty"`
	ExpiresAt int64  `json:"expires_at_ms"`
	CreatedAt int64  `json:"created_at_ms"`
	// KeyAvailable: the story carries media a per-story key applies to (the
	// client tracks actual key receipt — the server never holds the key).
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
