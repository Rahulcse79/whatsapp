// Package domain holds webinar/live-mode pure logic (T9.02): the role + status
// lattices, the publish gate (1-to-many: only host/speakers publish; attendees
// subscribe-only until promoted), waiting-room admission, raise-hand, and Q&A
// validation. No I/O. Media rides the existing LiveKit plane; role-scoped join
// tokens (canPublish) enforce the 1-to-many shape.
package domain

import (
	"errors"
	"strings"
)

// Role in a webinar: attendee (watch-only), speaker (may publish), host (runs it).
type Role int16

const (
	RoleAttendee Role = 0
	RoleSpeaker  Role = 1
	RoleHost     Role = 2
)

func (r Role) Valid() bool { return r >= RoleAttendee && r <= RoleHost }

func (r Role) String() string {
	switch r {
	case RoleHost:
		return "host"
	case RoleSpeaker:
		return "speaker"
	default:
		return "attendee"
	}
}

// Status is an attendee's lifecycle in the room.
type Status int16

const (
	StatusWaiting  Status = 0 // in the waiting room, not yet admitted
	StatusAdmitted Status = 1 // admitted — may fetch a join token
	StatusLeft     Status = 2 // left / removed
)

func (s Status) String() string {
	switch s {
	case StatusAdmitted:
		return "admitted"
	case StatusLeft:
		return "left"
	default:
		return "waiting"
	}
}

// CanPublish is the 1-to-many gate: host + speakers publish; attendees don't
// (their join token is subscribe-only until promoted).
func CanPublish(r Role) bool { return r >= RoleSpeaker }

// CanHost gates host-only actions (admit/deny, promote/demote, Q&A moderation,
// end): host only.
func CanHost(r Role) bool { return r == RoleHost }

const (
	MaxTitle    = 120
	MaxQuestion = 500
)

var (
	ErrBadTitle    = errors.New("webinar: title is required (max 120 chars)")
	ErrBadQuestion = errors.New("webinar: question is required (max 500 chars)")
	ErrNotAdmitted = errors.New("webinar: not admitted yet")
)

// ValidateCreate checks a new webinar's title.
func ValidateCreate(title string) error {
	if strings.TrimSpace(title) == "" || len(title) > MaxTitle {
		return ErrBadTitle
	}
	return nil
}

// ValidateQuestion checks a Q&A submission.
func ValidateQuestion(body string) error {
	if strings.TrimSpace(body) == "" || len(body) > MaxQuestion {
		return ErrBadQuestion
	}
	return nil
}
