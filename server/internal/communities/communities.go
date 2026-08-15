// Package communities owns communities: a roof over many chat groups plus an
// announcement group, with its own membership, roles, shared events/calendar,
// moderation, and public discovery (T8.01). Group *messages* stay in the groups
// context (E2EE); this context stores only the community structure + metadata.
package communities

import (
	"context"
	"errors"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/communities/domain"
)

// ErrNotFound is returned when no community/member/event matches.
var ErrNotFound = errors.New("communities: not found")

// Community is the server-visible community record.
type Community struct {
	ID                  string
	Name                string
	Description         string
	Kind                domain.Kind
	OwnerID             string
	AnnouncementGroupID string
	CreatedAt           time.Time
}

// Member is a community membership row.
type Member struct {
	UserID string
	Role   domain.Role
}

// Event is a shared calendar entry.
type Event struct {
	ID          string
	CommunityID string
	Title       string
	Description string
	StartsAt    time.Time
	CreatedBy   string
	CreatedAt   time.Time
}

// CreateResult is the POST /v1/communities response.
type CreateResult struct {
	ID                  string `json:"id"`
	AnnouncementGroupID string `json:"announcement_group_id"`
}

// View is the GET /v1/communities/{id} response (caller-relative).
type View struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Description         string `json:"description,omitempty"`
	Kind                string `json:"kind"`
	AnnouncementGroupID string `json:"announcement_group_id"`
	MemberCount         int    `json:"member_count"`
	GroupCount          int    `json:"group_count"`
	MyRole              string `json:"my_role,omitempty"` // "" when not a member
}

// Summary is one discover/search row.
type Summary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MemberCount int    `json:"member_count"`
}

// EventView is one calendar entry over the wire.
type EventView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	StartsAtMS  int64  `json:"starts_at_ms"`
	CreatedBy   string `json:"created_by"`
}

// GroupCreator creates the community's announcement group in the groups context
// (kept as a port so communities doesn't depend on groups directly). The owner
// becomes the group's owner.
type GroupCreator interface {
	CreateAnnouncementGroup(ctx context.Context, ident auth.Identity, name string) (groupID string, err error)
}

// Store persists communities + members + group links + events.
type Store interface {
	Create(ctx context.Context, c Community) error
	Get(ctx context.Context, id string) (Community, error) // ErrNotFound
	Delete(ctx context.Context, id string) error
	Counts(ctx context.Context, id string) (members int, groups int, err error)

	AddMember(ctx context.Context, communityID, userID string, role domain.Role) error
	RemoveMember(ctx context.Context, communityID, userID string) error
	GetMember(ctx context.Context, communityID, userID string) (Member, error) // ErrNotFound
	ListMembers(ctx context.Context, communityID string) ([]Member, error)
	SetRole(ctx context.Context, communityID, userID string, role domain.Role) error

	AddGroup(ctx context.Context, communityID, groupID string) error
	RemoveGroup(ctx context.Context, communityID, groupID string) error
	ListGroups(ctx context.Context, communityID string) ([]string, error)

	CreateEvent(ctx context.Context, e Event) error
	ListEvents(ctx context.Context, communityID string, from time.Time) ([]Event, error)
	DeleteEvent(ctx context.Context, communityID, eventID string) error

	// Discover lists public communities by member count; Search matches name.
	Discover(ctx context.Context, limit int) ([]Summary, error)
	Search(ctx context.Context, query string, limit int) ([]Summary, error)
}
