// Package groups owns membership, roles, permissions, invite links/QR, and
// group metadata. Every membership/settings mutation bumps groups.version and
// emits an ordered group event — clients rotate Sender Keys on those events;
// this package never touches key material.
//
// Design: Docs/04-api/messaging-groups-api.md, Docs/06-security/e2ee-design.md §3.
package groups

import (
	"time"

	"github.com/whatsapp-v2/server/internal/groups/domain"
)

// Group is a groups row.
type Group struct {
	ID          string
	Name        string
	Description string
	AvatarRef   string
	Settings    domain.Settings
	Version     int64
	CreatedBy   string
	CreatedAt   time.Time
}

// Member is a group_members row.
type Member struct {
	GroupID  string
	UserID   string
	Role     domain.Role
	JoinedAt time.Time
}

// InviteLink is an invite_links row: a capability token that grants join.
type InviteLink struct {
	Token     string
	GroupID   string
	CreatedBy string
	ExpiresAt *time.Time
	RevokedAt *time.Time
	MaxUses   *int
	Uses      int
}

// ── client-facing views (JSON) ──────────────────────────────────────────────

// GroupView is the shape returned by GET/POST group endpoints.
type GroupView struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Settings    domain.Settings `json:"settings"`
	Version     int64           `json:"version"`
	MyRole      string          `json:"my_role,omitempty"`
}

// MemberView is one paged member row.
type MemberView struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

func (g Group) view(myRole string) GroupView {
	return GroupView{
		ID:          g.ID,
		Name:        g.Name,
		Description: g.Description,
		Settings:    g.Settings,
		Version:     g.Version,
		MyRole:      myRole,
	}
}
