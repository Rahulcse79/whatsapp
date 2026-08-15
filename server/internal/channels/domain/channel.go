// Package domain holds the channels context's pure logic: roles + the channel
// permission matrix, the public/private kind, and field validation. No I/O —
// enforced by depguard and unit-tested. Channels are a broadcast plane (content
// is server-visible), so there is no key material here.
package domain

import "errors"

// Role mirrors channel_members.role: 0 follower | 1 admin | 2 owner.
type Role int16

const (
	RoleFollower Role = 0
	RoleAdmin    Role = 1
	RoleOwner    Role = 2
)

func (r Role) Valid() bool { return r >= RoleFollower && r <= RoleOwner }

func (r Role) String() string {
	switch r {
	case RoleOwner:
		return "owner"
	case RoleAdmin:
		return "admin"
	default:
		return "follower"
	}
}

// Assignable is the set a role can be set to via the role endpoint — never owner
// (ownership is fixed at creation; transfer is out of scope for V3).
func (r Role) Assignable() bool { return r == RoleFollower || r == RoleAdmin }

// Kind is a channel's visibility. Public channels are discoverable and anyone
// may follow; private channels are hidden from discovery and (in this V3 slice)
// only the owner/admins may add — following a private channel is invite-only,
// modelled as an admin promoting a follower in.
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

// ── permission matrix ───────────────────────────────────────────────────────

// CanPost: publish/schedule/delete posts — admin or owner.
func CanPost(r Role) bool { return r >= RoleAdmin }

// CanManage: edit channel info, manage members/roles gate below — admin+.
func CanEditInfo(r Role) bool { return r >= RoleAdmin }

// CanChangeRoles: promote/demote admins — owner only.
func CanChangeRoles(r Role) bool { return r == RoleOwner }

// CanDelete: delete the whole channel — owner only.
func CanDelete(r Role) bool { return r == RoleOwner }

// ── validation ──────────────────────────────────────────────────────────────

const (
	MaxName        = 80
	MaxDescription = 500
	MaxPostBody    = 4096
	MaxCommentBody = 2048
)

var (
	ErrBadHandle  = errors.New("channels: handle must be 3–30 chars of a–z, 0–9 or _")
	ErrBadName    = errors.New("channels: name is required (max 80 chars)")
	ErrBadDesc    = errors.New("channels: description too long (max 500 chars)")
	ErrBadKind    = errors.New("channels: kind must be public or private")
	ErrBadPost    = errors.New("channels: post body required (max 4096 chars)")
	ErrBadComment = errors.New("channels: comment required (max 2048 chars)")
)

// ValidHandle checks a channel's public @handle (3–30 of [a-z0-9_], case-folded
// by the caller).
func ValidHandle(h string) bool {
	if len(h) < 3 || len(h) > 30 {
		return false
	}
	for _, c := range h {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// ValidateCreate checks a new channel's fields.
func ValidateCreate(handle, name, description string, kind Kind) error {
	if !ValidHandle(handle) {
		return ErrBadHandle
	}
	if name == "" || len(name) > MaxName {
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

// ValidatePost checks a post body.
func ValidatePost(body string) error {
	if body == "" || len(body) > MaxPostBody {
		return ErrBadPost
	}
	return nil
}

// ValidateComment checks a comment body.
func ValidateComment(body string) error {
	if body == "" || len(body) > MaxCommentBody {
		return ErrBadComment
	}
	return nil
}
