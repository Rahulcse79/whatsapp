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
	Premium     bool
	PriceCents  int
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
	Views     int64
	CreatedAt time.Time
}

// Insights is a channel's aggregate, privacy-preserving analytics (channel
// admin only) — sums over the channel's own content + audience, no per-viewer
// data. "Reach" is the total post views (impressions).
type Insights struct {
	ChannelID   string `json:"channel_id"`
	Followers   int    `json:"followers"`
	Subscribers int    `json:"subscribers"`
	Posts       int    `json:"posts"`
	Views       int64  `json:"views"` // total impressions across posts (= reach)
	Reactions   int    `json:"reactions"`
	Comments    int    `json:"comments"`
	Premium     bool   `json:"premium"`
	PriceCents  int    `json:"price_cents"`
}

// SubscribeResult is the POST /subscribe response (the payment ref comes from
// the external processor via the PaymentGateway seam).
type SubscribeResult struct {
	PaymentRef  string `json:"payment_ref"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
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
	ID           string `json:"id"`
	Handle       string `json:"handle"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Kind         string `json:"kind"` // public | private
	Verified     bool   `json:"verified"`
	Followers    int    `json:"followers"`
	MyRole       string `json:"my_role,omitempty"` // "" if not a member
	Premium      bool   `json:"premium"`
	PriceCents   int    `json:"price_cents"`
	MySubscribed bool   `json:"my_subscribed"` // caller has an active subscription
	CreatedAt    int64  `json:"created_at_ms"`
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

	// analytics + monetization (T7.03)
	IncrementViews(ctx context.Context, postID string) error
	Insights(ctx context.Context, channelID string) (Insights, error)
	SetPremium(ctx context.Context, channelID string, premium bool, priceCents int) error
	Subscribe(ctx context.Context, channelID, userID, paymentRef string, expiresAt time.Time) error
	// IsSubscribed reports whether the user has an unexpired subscription.
	IsSubscribed(ctx context.Context, channelID, userID string, now time.Time) (bool, error)
}

// Broadcaster notifies online followers of a new post over NATS → WS gateway
// (the real-time seam; the durable path is followers pulling ListPosts).
type Broadcaster interface {
	PostPublished(ctx context.Context, channelID, postID string) error
}

// PaymentGateway is the monetization SEAM: charging a subscriber runs through an
// external processor. The dev/self-host build uses a Noop that records a
// placeholder reference and never moves money (per the project's no-payments
// stance). A real deployment injects a Stripe/… adapter here.
type PaymentGateway interface {
	// Charge attempts a monthly charge and returns the processor's reference.
	Charge(ctx context.Context, userID, channelID string, cents int) (ref string, err error)
}
