// Package domain holds the multi-channel notification decision logic (T14.01):
// the channel model, quiet-hours math, and the pure Route() engine that decides
// which channels a wake/nudge may use for a given event. No I/O. Preferences and
// routing never touch message content — they only gate content-free signals, so
// the E2EE no-plaintext invariant (FR-NOTIF-01) is preserved.
package domain

import "errors"

// Channel is one delivery lane. Values are bit positions so a user's enabled set
// is a compact bitmask (notification_prefs.channels).
type Channel uint8

const (
	ChannelPush    Channel = 1 << 0 // device push (FCM/ntfy/APNs/WebPush) — the primary lane
	ChannelEmail   Channel = 1 << 1 // content-free email nudge (fallback)
	ChannelSMS     Channel = 1 << 2 // content-free SMS nudge (last-resort fallback)
	ChannelDesktop Channel = 1 << 3 // desktop/browser notification while a client is foregrounded
)

// allChannels is every valid bit — used to reject unknown bits.
const allChannels = ChannelPush | ChannelEmail | ChannelSMS | ChannelDesktop

// DefaultMask is what a user gets before they customise anything: push + desktop
// (the two zero-cost, content-free lanes). Email/SMS are opt-in.
const DefaultMask = ChannelPush | ChannelDesktop

// Kind mirrors notify.Kind — the wake reason. A call always breaks through mute
// and quiet hours; messages and generic events respect them.
type Kind int16

const (
	KindMessage Kind = 1
	KindCall    Kind = 2
	KindGeneric Kind = 3
)

// Prefs is a user's notification routing configuration.
type Prefs struct {
	Channels   Channel // enabled-lane bitmask
	QuietStart int     // minute-of-day [0,1439]; -1 when quiet hours are off
	QuietEnd   int     // minute-of-day [0,1439]; -1 when quiet hours are off
	Sound      bool
	Vibrate    bool
}

// DefaultPrefs is the server-side default for a user with no stored row.
func DefaultPrefs() Prefs {
	return Prefs{Channels: DefaultMask, QuietStart: -1, QuietEnd: -1, Sound: true, Vibrate: true}
}

var (
	ErrBadChannels = errors.New("notifyprefs: unknown channel bits")
	ErrBadQuiet    = errors.New("notifyprefs: quiet hours must be two minute-of-day values in [0,1439], or both off")
)

// Has reports whether a channel bit is set.
func (p Prefs) Has(c Channel) bool { return p.Channels&c != 0 }

// QuietHoursOn reports whether a quiet-hours window is configured.
func (p Prefs) QuietHoursOn() bool { return p.QuietStart >= 0 && p.QuietEnd >= 0 }

// Validate checks a Prefs before persisting.
func Validate(p Prefs) error {
	if p.Channels&^allChannels != 0 {
		return ErrBadChannels
	}
	// Quiet hours are all-or-nothing; each endpoint must be a valid minute-of-day.
	offStart, offEnd := p.QuietStart < 0, p.QuietEnd < 0
	if offStart != offEnd {
		return ErrBadQuiet
	}
	if !offStart {
		if p.QuietStart > 1439 || p.QuietEnd > 1439 {
			return ErrBadQuiet
		}
	}
	return nil
}

// InQuietHours reports whether localMin (minute-of-day, [0,1439]) falls inside
// the configured quiet window. The window may wrap past midnight
// (e.g. 22:00→07:00). A start == end window means "all day". Off ⇒ never quiet.
func (p Prefs) InQuietHours(localMin int) bool {
	if !p.QuietHoursOn() {
		return false
	}
	s, e := p.QuietStart, p.QuietEnd
	if s == e {
		return true // 24h quiet
	}
	if s < e {
		return localMin >= s && localMin < e // same-day window
	}
	return localMin >= s || localMin < e // wraps past midnight
}

// Route decides which channels an event may use, most-preferred first (push,
// email, sms, desktop). Rules:
//   - A call (KindCall) always breaks through: it ignores snooze and quiet hours
//     and returns every enabled channel (a missed call must reach the user).
//   - Otherwise, if the conversation is snoozed (nowMS < mutedUntilMS) or the
//     user is in quiet hours, NO channel fires.
//   - Otherwise, every enabled channel fires.
//
// mutedUntilMS is 0 when the conversation is not snoozed. localMin is the user's
// current minute-of-day (the caller converts nowMS to the user's timezone; a
// server with no tz uses UTC — a documented approximation).
func Route(p Prefs, mutedUntilMS, nowMS int64, localMin int, kind Kind) []Channel {
	order := []Channel{ChannelPush, ChannelEmail, ChannelSMS, ChannelDesktop}
	enabled := make([]Channel, 0, len(order))
	for _, c := range order {
		if p.Has(c) {
			enabled = append(enabled, c)
		}
	}
	if kind == KindCall {
		return enabled // breaks through everything
	}
	if nowMS < mutedUntilMS { // conversation snoozed
		return nil
	}
	if p.InQuietHours(localMin) {
		return nil
	}
	return enabled
}
