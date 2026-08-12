// Package domain holds stories' pure logic: the kind taxonomy, the 24 h expiry,
// and the audience-snapshot rules. No I/O. Content is E2E-encrypted with a
// per-story key distributed client-side (WS MsgSend{STORY_KEY}); the server only
// ever holds the ciphertext media ref + this metadata.
package domain

import (
	"errors"
	"time"
)

// TTL is a story's lifetime: post + 24 h, then hard-deleted (a purge job, with
// MinIO ILM as the media backstop).
const TTL = 24 * time.Hour

// Kind is what a story carries (stored as call it smallint on the wire).
type Kind int16

const (
	KindImage Kind = 0
	KindVideo Kind = 1
	KindText  Kind = 2
)

var (
	ErrBadKind      = errors.New("stories: kind must be image, video, or text")
	ErrMediaMissing = errors.New("stories: image/video story needs a media ref")
	ErrMediaOnText  = errors.New("stories: text story must not carry a media ref")
	ErrNoAudience   = errors.New("stories: audience is empty")
)

// ParseKind maps the wire string to a Kind.
func ParseKind(s string) (Kind, bool) {
	switch s {
	case "image":
		return KindImage, true
	case "video":
		return KindVideo, true
	case "text":
		return KindText, true
	default:
		return 0, false
	}
}

func (k Kind) String() string {
	switch k {
	case KindImage:
		return "image"
	case KindVideo:
		return "video"
	case KindText:
		return "text"
	default:
		return "unknown"
	}
}

func (k Kind) needsMedia() bool { return k == KindImage || k == KindVideo }

// ValidateCreate checks kind/media consistency and a non-empty audience.
func ValidateCreate(kind Kind, hasMediaRef bool, audienceSize int) error {
	if kind != KindImage && kind != KindVideo && kind != KindText {
		return ErrBadKind
	}
	if kind.needsMedia() && !hasMediaRef {
		return ErrMediaMissing
	}
	if !kind.needsMedia() && hasMediaRef {
		return ErrMediaOnText
	}
	if audienceSize == 0 {
		return ErrNoAudience
	}
	return nil
}

// ExpiryFrom returns the hard-expiry instant for a story posted at `now`.
func ExpiryFrom(now time.Time) time.Time { return now.Add(TTL) }

// Audience finalizes the eligible-viewer set at post time: the override if the
// author supplied one, else the author's contacts; the author is always included
// (they see their own story), and duplicates are removed. Order is not
// significant (the feed query is a set membership test).
func Audience(authorID string, override, contacts []string) []string {
	source := contacts
	if override != nil {
		source = override
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(source)+1)
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(authorID)
	for _, id := range source {
		add(id)
	}
	return out
}
