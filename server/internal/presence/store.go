package presence

import (
	"context"
	"time"
)

// OnlineWindow is how long a device is considered online after its last
// heartbeat (valkey-keyspace.md: presence 60 s sliding). A device that stops
// heartbeating for longer is pruned and counts as gone.
const OnlineWindow = 60 * time.Second

// Update is a presence change fanned out to a user's subscribers.
type Update struct {
	UserID     string
	Online     bool
	LastSeenMS int64 // 0 when hidden by privacy (wire contract: PresenceUpdate)
}

// TypingEvent is a typing/recording indicator from one user, relayed to their
// subscribers; the client shows it against the matching open conversation.
type TypingEvent struct {
	TyperUserID    string
	ConversationID string
	Recording      bool
}

// Store holds per-user online state in Valkey as a set of currently-connected
// devices (multi-device: a user is online while any device is). Stale devices
// (no heartbeat within OnlineWindow) are pruned on every operation so a
// crashed pod's connections don't pin a user online forever.
type Store interface {
	// Connect registers a device; becameOnline is true only on the offline→
	// online transition (the user's first live device), which is what warrants
	// a fan-out.
	Connect(ctx context.Context, userID, deviceID string, nowMS int64) (becameOnline bool, err error)
	// Heartbeat refreshes a device's liveness.
	Heartbeat(ctx context.Context, userID, deviceID string, nowMS int64) error
	// Disconnect removes a device; becameOffline is true only when the last
	// device leaves.
	Disconnect(ctx context.Context, userID, deviceID string, nowMS int64) (becameOffline bool, err error)
	// Snapshot returns a user's current presence, for the initial state pushed
	// when a subscriber starts tracking them.
	Snapshot(ctx context.Context, userID string, nowMS int64) (Update, error)
}

// Publisher fans presence and typing to a user's subscribers over NATS
// (pres.{user}, typ.{user}).
type Publisher interface {
	PublishUpdate(ctx context.Context, u Update) error
	PublishTyping(ctx context.Context, t TypingEvent) error
}

// PrivacyChecker enforces last-seen / online visibility at subscribe time
// (FR-USER-02). In production this is a cached core-api lookup; AllowAll is the
// interim default until that wiring lands.
type PrivacyChecker interface {
	// CanSeePresence reports whether viewer may see target's presence.
	CanSeePresence(ctx context.Context, viewerUserID, targetUserID string) (bool, error)
}

// AllowAllPrivacy permits every subscription — interim default.
type AllowAllPrivacy struct{}

func (AllowAllPrivacy) CanSeePresence(context.Context, string, string) (bool, error) {
	return true, nil
}
