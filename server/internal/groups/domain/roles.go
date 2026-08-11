// Package domain holds the groups context's pure logic: roles, settings, and
// the permission matrix. No I/O — enforced by depguard (domain-stays-pure) and
// unit-tested to the ≥90% bar (test-strategy §2).
package domain

// Role mirrors the group_members.role column (0 member | 1 admin | 2 owner).
type Role int16

const (
	RoleMember Role = 0
	RoleAdmin  Role = 1
	RoleOwner  Role = 2
)

// MaxMembers caps a group (FR-GRP; messaging-groups-api.md, STATE_GROUP_FULL).
const MaxMembers = 1024

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

// Assignable is the set a role can be set to via PUT role (never owner — owner
// transfer goes through leave, messaging-groups-api.md).
func (r Role) Assignable() bool { return r == RoleMember || r == RoleAdmin }

// Policy values for the who-can-* settings.
const (
	PolicyAll    = "all"
	PolicyAdmins = "admins"
)

// Settings is the groups.settings JSON (who_can_post, who_can_edit_info,
// announcements). Announcements mode makes the group admin-post-only.
type Settings struct {
	WhoCanPost     string `json:"who_can_post"`
	WhoCanEditInfo string `json:"who_can_edit_info"`
	Announcements  bool   `json:"announcements"`
}

// DefaultSettings are applied at creation: open posting, admin-only info edits.
func DefaultSettings() Settings {
	return Settings{WhoCanPost: PolicyAll, WhoCanEditInfo: PolicyAdmins, Announcements: false}
}

func validPolicy(p string) bool { return p == PolicyAll || p == PolicyAdmins }

// Valid reports whether the settings are well-formed.
func (s Settings) Valid() bool {
	return validPolicy(s.WhoCanPost) && validPolicy(s.WhoCanEditInfo)
}

// ── permission matrix (the whole point of this package) ─────────────────────

// CanManageMembers: add/remove members — admin or owner.
func CanManageMembers(r Role) bool { return r >= RoleAdmin }

// CanChangeRoles: promote/demote — owner only.
func CanChangeRoles(r Role) bool { return r == RoleOwner }

// CanDeleteGroup: delete the whole group — owner only.
func CanDeleteGroup(r Role) bool { return r == RoleOwner }

// CanEditSettings: change who_can_* / announcements — admin or owner.
func CanEditSettings(r Role) bool { return r >= RoleAdmin }

// CanEditInfo: name/description/avatar — gated by who_can_edit_info.
func CanEditInfo(r Role, s Settings) bool {
	if s.WhoCanEditInfo == PolicyAll {
		return true
	}
	return r >= RoleAdmin
}

// CanPost: send to the group's conversation — gated by announcements +
// who_can_post (GROUP_POSTING_RESTRICTED otherwise).
func CanPost(r Role, s Settings) bool {
	if s.Announcements || s.WhoCanPost == PolicyAdmins {
		return r >= RoleAdmin
	}
	return true
}

// CanRemove reports whether an actor may remove target. Admins may remove
// members; only an owner may remove an admin; nobody removes the owner (the
// owner leaves via transfer). Self-removal is handled by leave, not here.
func CanRemove(actor, target Role) bool {
	if target == RoleOwner {
		return false
	}
	if target == RoleAdmin {
		return actor == RoleOwner
	}
	return actor >= RoleAdmin
}
