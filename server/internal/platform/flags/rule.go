// Package flags is the feature-flag + kill-switch layer (core-api-lld §5):
// rules stored in feature_flags, evaluated through a 30 s Valkey cache. Two
// shapes ride the same rule: gradual feature gates (fail-closed — a feature a
// pod can't confirm stays OFF) and operational kill-switches for incident
// response (fail-open — a flag store hiccup must never accidentally pause the
// whole product). The asymmetry falls out of Enabled vs Allowed, below.
package flags

import "hash/fnv"

// Rule is the JSONB stored per flag. Evaluation order: master switch, then the
// per-subject deny/allow overrides, then the percentage rollout. A zero Rule
// (all fields empty) evaluates to false — a disabled flag.
type Rule struct {
	Enabled bool     `json:"enabled"`         // master on/off
	Rollout int      `json:"rollout"`         // 0–100, percentage of subjects
	Allow   []string `json:"allow,omitempty"` // subjects always on (beats rollout)
	Deny    []string `json:"deny,omitempty"`  // subjects always off (beats everything)
}

// Eval decides whether the flag is on for subject. The flag name is folded into
// the rollout hash so two flags at the same percentage target independent
// cohorts (a user isn't in "the 10 %" for every flag at once). An empty subject
// (e.g. a global kill-switch checked without a user) skips the allow/deny lists
// and rides Enabled + the rollout edges (0 → off, ≥100 → on).
func (r Rule) Eval(flag, subject string) bool {
	if !r.Enabled {
		return false
	}
	if subject != "" {
		if contains(r.Deny, subject) {
			return false
		}
		if contains(r.Allow, subject) {
			return true
		}
	}
	switch {
	case r.Rollout <= 0:
		return false
	case r.Rollout >= 100:
		return true
	default:
		return bucket(flag, subject) < r.Rollout
	}
}

// bucket maps (flag, subject) to a stable [0,100) slot — the same subject always
// lands in the same slot for a given flag, so a rollout only ever grows the
// cohort, never reshuffles it.
func bucket(flag, subject string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(flag))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(subject))
	return int(h.Sum32() % 100)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
