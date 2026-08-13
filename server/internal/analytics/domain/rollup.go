// Package domain holds analytics' pure aggregation logic (HLD §18.1): the
// metadata-only event taxonomy, UTC day bucketing, and the retention window.
// Metadata only, by construction — an Event has no content field, and distinct
// counting (DAU/MAU) rides a probabilistic sketch so no per-user row is ever
// stored. No I/O here.
package domain

import "time"

// EventKind enumerates the product signals we aggregate. Every kind is a count
// or a distinct-user tick — never anything derived from message content.
type EventKind string

const (
	KindSignup         EventKind = "signup"          // new account created
	KindActiveUser     EventKind = "active_user"     // a user was active today (→ DAU/MAU)
	KindMessageRelayed EventKind = "message_relayed" // one message relayed (count only)
	KindCallStarted    EventKind = "call_started"    // a call was set up
	KindCallMinutes    EventKind = "call_minutes"    // call duration, in minutes (Delta)
	KindFlagExposure   EventKind = "flag_exposure"   // a flag was evaluated on (adoption)
)

// Known reports whether k is a recognised kind (input validation at the edge).
func (k EventKind) Known() bool {
	switch k {
	case KindSignup, KindActiveUser, KindMessageRelayed, KindCallStarted, KindCallMinutes, KindFlagExposure:
		return true
	default:
		return false
	}
}

// Distinct reports whether the kind is counted by distinct users (HLL) rather
// than a plain running total. Only active users are counted distinctly.
func (k EventKind) Distinct() bool { return k == KindActiveUser }

// CounterMetric maps a counter kind (+ optional low-cardinality label) to its
// rollup metric name. Distinct kinds return ("", false). The label is folded in
// only for flag exposure (the flag name) — a bounded dimension, never PII.
func (k EventKind) CounterMetric(label string) (string, bool) {
	switch k {
	case KindSignup:
		return "signups", true
	case KindMessageRelayed:
		return "messages_relayed", true
	case KindCallStarted:
		return "calls_started", true
	case KindCallMinutes:
		return "call_minutes", true
	case KindFlagExposure:
		if label == "" {
			return "flag_exposure", true
		}
		return "flag_exposure:" + label, true
	default:
		return "", false
	}
}

// Day truncates t to midnight UTC — the bucket key for a daily rollup. All
// bucketing is UTC so pods in different zones agree on "today".
func Day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DayKey is the string form of a day bucket (HLL sketch keys, YYYY-MM-DD).
func DayKey(t time.Time) string { return Day(t).Format("2006-01-02") }

// TrailingDays returns n day buckets ending at (and including) day, most recent
// first — the window whose distinct union is the trailing-n-day active count
// (MAU = TrailingDays(today, 30)).
func TrailingDays(day time.Time, n int) []time.Time {
	day = Day(day)
	out := make([]time.Time, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, day.AddDate(0, 0, -i))
	}
	return out
}

// MAUWindow is the trailing window (days) for monthly-active-user counting.
const MAUWindow = 30

// RollupRetentionDays bounds how long daily rollups (and their HLL sketches)
// are kept — ~13 months, enough for year-over-year without unbounded growth.
const RollupRetentionDays = 400

// RetentionCutoff is the oldest day to keep as of now; older rollups are purged.
func RetentionCutoff(now time.Time) time.Time {
	return Day(now).AddDate(0, 0, -RollupRetentionDays)
}
