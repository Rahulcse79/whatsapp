// Package domain holds communities' pure logic: the role lattice + permission
// matrix, visibility, and field/event validation. No I/O. A community groups
// many chat groups under one roof plus an announcement group, with its own
// membership and roles (HLD §community; mirrors the channels role model).
package domain

import (
	"errors"
	"strings"
	"time"
)

// Role mirrors community_members.role: 0 member | 1 admin | 2 owner.
type Role int16

const (
	RoleMember Role = 0
	RoleAdmin  Role = 1
	RoleOwner  Role = 2
)

func (r Role) Valid() bool { return r >= RoleMember && r <= RoleOwner }

func (r Role) String() string {
	switch r {
	case RoleOwner:
		return "owner"
	case RoleAdmin:
		return "admin"
	default:
		return "member"
	}
}

// Assignable roles a manager may set (owner is transferred, not assigned here).
func (r Role) Assignable() bool { return r == RoleMember || r == RoleAdmin }

// Kind is a community's visibility. Public communities are discoverable and
// join-anyone; private ones are invite-only.
type Kind int16

const (
	KindPublic  Kind = 0
	KindPrivate Kind = 1
)

func (k Kind) Valid() bool { return k == KindPublic || k == KindPrivate }

func (k Kind) String() string {
	if k == KindPrivate {
		return "private"
	}
	return "public"
}

// ── permission matrix ────────────────────────────────────────────────────────

// CanManageGroups: link/unlink member groups — admin or owner.
func CanManageGroups(r Role) bool { return r >= RoleAdmin }

// CanModerate: remove members, manage events — admin or owner.
func CanModerate(r Role) bool { return r >= RoleAdmin }

// CanChangeRoles: promote/demote admins — owner only.
func CanChangeRoles(r Role) bool { return r == RoleOwner }

// CanDelete: delete the whole community — owner only.
func CanDelete(r Role) bool { return r == RoleOwner }

// ── validation ────────────────────────────────────────────────────────────────

const (
	MaxName        = 80
	MaxDescription = 500
	MaxEventTitle  = 120
	MaxEventDesc   = 1000
)

var (
	ErrBadName       = errors.New("communities: name is required (max 80 chars)")
	ErrBadDesc       = errors.New("communities: description too long (max 500 chars)")
	ErrBadKind       = errors.New("communities: kind must be public or private")
	ErrBadEventTitle = errors.New("communities: event title is required (max 120 chars)")
	ErrBadEventDesc  = errors.New("communities: event description too long (max 1000 chars)")
	ErrBadEventTime  = errors.New("communities: event start time is required")
)

// ValidateCreate checks a new community's fields.
func ValidateCreate(name, description string, kind Kind) error {
	if strings.TrimSpace(name) == "" || len(name) > MaxName {
		return ErrBadName
	}
	if len(description) > MaxDescription {
		return ErrBadDesc
	}
	if !kind.Valid() {
		return ErrBadKind
	}
	return nil
}

// ValidateEvent checks a calendar event's fields.
func ValidateEvent(title, description string, startsAt time.Time) error {
	if strings.TrimSpace(title) == "" || len(title) > MaxEventTitle {
		return ErrBadEventTitle
	}
	if len(description) > MaxEventDesc {
		return ErrBadEventDesc
	}
	if startsAt.IsZero() {
		return ErrBadEventTime
	}
	return nil
}
