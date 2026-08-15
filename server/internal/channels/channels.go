// Package channels owns broadcast publishing: public/private/verified channels,
// followers, posts (immediate + scheduled), reactions, comments, and channel
// admin roles (T7.01). Unlike the E2EE messaging plane, a channel post's content
// is server-visible — a channel broadcasts to an unbounded follower set, so the
// content rides in plaintext and access is controlled by kind + membership. On
// publish the service emits a NATS broadcast event so the WS gateway can notify
// online followers; the primary delivery path is followers pulling the feed.
package channels

import (
	"context"
	"errors"
	"time"

	"github.com/whatsapp-v2/server/internal/channels/domain"
)

var (
	// ErrNotFound is returned when no channel/post/comment matches.
	ErrNotFound = errors.New("channels: not found")
	// ErrHandleTaken is returned when a channel handle is already in use.
	ErrHandleTaken = errors.New("channels: handle already taken")
)

// Channel is a channels row.
type Channel struct {
	ID          string
	OwnerID     string
	Handle      string
	Name        string
	Description string
	Kind        domain.Kind
	Verified    bool
	CreatedAt   time.Time
}

// Member is a channel_members row (follower or admin/owner).
type Member struct {
	ChannelID string
	UserID    string
	Role      domain.Role
	JoinedAt  time.Time
}

// Post is a channel_posts row.
type Post struct {
	ID        string
	ChannelID string
	AuthorID  string
	Body      string
	MediaRef  *string
	PublishAt time.Time
	Published bool
	CreatedAt time.Time
}

// Comment is a channel_comments row.
type Comment struct {
	ID        string
	PostID    string
	AuthorID  string
	Body      string
	CreatedAt time.Time
}

// ── client-facing views (JSON) ──────────────────────────────────────────────

// ChannelView is returned by the channel endpoints.
type ChannelView struct {
	ID          string `json:"id"`
	Handle      string `json:"handle"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"` // public | private
	Verified    bool   `json:"verified"`
	Followers   int    `json:"followers"`
	MyRole      string `json:"my_role,omitempty"` // "" if not a member
	CreatedAt   int64  `json:"created_at_ms"`
}

// PostView is one feed entry.
type PostView struct {
	ID        string         `json:"id"`
	ChannelID string         `json:"channel_id"`
	Body      string         `json:"body"`
	MediaRef  string         `json:"media_ref,omitempty"`
	Scheduled bool           `json:"scheduled,omitempty"` // future publish_at, not yet live
	PublishAt int64          `json:"publish_at_ms"`
	Reactions map[string]int `json:"reactions,omitempty"` // emoji → count
	Comments  int            `json:"comments"`
	CreatedAt int64          `json:"created_at_ms"`
}

// CommentView is one comment.
type CommentView struct {
	ID        string `json:"id"`
	AuthorID  string `json:"author_id"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at_ms"`
}

// MemberView is one follower/admin row.
type MemberView struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// ── ports ───────────────────────────────────────────────────────────────────

// Store persists channels + members + posts + reactions + comments.
type Store interface {
	CreateChannel(ctx context.Context, c Channel) error // ErrHandleTaken
	GetChannel(ctx context.Context, id string) (Channel, error)
	UpdateChannel(ctx context.Context, id string, name, description *string) error
	DeleteChannel(ctx context.Context, id string) error
	SearchChannels(ctx context.Context, query string, limit int) ([]Channel, error) // public, discoverable
	Discover(ctx context.Context, limit int) ([]Channel, error)                     // public, by follower count
	FollowerCount(ctx context.Context, channelID string) (int, error)

	GetMember(ctx context.Context, channelID, userID string) (Member, error) // ErrNotFound if not a member
	AddMember(ctx context.Context, m Member) error                           // idempotent follow
	RemoveMember(ctx context.Context, channelID, userID string) error
	SetRole(ctx context.Context, channelID, userID string, role domain.Role) error // ErrNotFound if not a member
	ListMembers(ctx context.Context, channelID string, limit int) ([]Member, error)

	CreatePost(ctx context.Context, p Post) error
	GetPost(ctx context.Context, postID string) (Post, error)
	ListPosts(ctx context.Context, channelID string, limit int) ([]Post, error) // published, newest first
	DeletePost(ctx context.Context, postID string) error
	// PublishDue flips scheduled posts whose publish_at has passed to published
	// and returns them (for the broadcast fan-out).
	PublishDue(ctx context.Context, now time.Time, limit int) ([]Post, error)

	React(ctx context.Context, postID, userID, emoji string) error
	Unreact(ctx context.Context, postID, userID, emoji string) error
	Reactions(ctx context.Context, postID string) (map[string]int, error)

	CreateComment(ctx context.Context, c Comment) error
	ListComments(ctx context.Context, postID string, limit int) ([]Comment, error)
	CommentCount(ctx context.Context, postID string) (int, error)
	GetComment(ctx context.Context, id string) (Comment, error)
	DeleteComment(ctx context.Context, id string) error
}

// Broadcaster notifies online followers of a new post over NATS → WS gateway
// (the real-time seam; the durable path is followers pulling ListPosts).
type Broadcaster interface {
	PostPublished(ctx context.Context, channelID, postID string) error
}
