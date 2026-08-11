package groups

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/groups/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// ErrNotFound is returned by the Repo when no row matches.
var ErrNotFound = errors.New("groups: not found")

// Repo is the groups persistence port. Mutations that change membership or
// settings bump groups.version atomically and return the new version so the
// service can stamp the emitted event (Sender-Key rotation ordering).
type Repo interface {
	CreateGroup(ctx context.Context, g Group, members []Member) error
	GetGroup(ctx context.Context, groupID string) (Group, error)
	GetMember(ctx context.Context, groupID, userID string) (Member, error)
	ListMembers(ctx context.Context, groupID, afterUserID string, limit int) ([]Member, error)
	CountMembers(ctx context.Context, groupID string) (int, error)
	UpdateInfo(ctx context.Context, groupID string, name, description, avatarRef *string) (int64, error)
	UpdateSettings(ctx context.Context, groupID string, s domain.Settings) (int64, error)
	// AddMembers inserts the not-yet-present users as members and returns the
	// ones actually added plus the new version.
	AddMembers(ctx context.Context, groupID string, userIDs []string) (added []string, version int64, err error)
	RemoveMember(ctx context.Context, groupID, userID string) (version int64, removed bool, err error)
	SetRole(ctx context.Context, groupID, userID string, role domain.Role) (int64, error)
	DeleteGroup(ctx context.Context, groupID string) error
	CreateInvite(ctx context.Context, l InviteLink) error
	GetInvite(ctx context.Context, token string) (InviteLink, error)
	RevokeInvite(ctx context.Context, token string) error
	// JoinViaInvite atomically validates + consumes the invite and adds the
	// member, returning the group and the new version. Rejections come back as
	// sentinel errors (ErrInviteInvalid / ErrGroupFull).
	JoinViaInvite(ctx context.Context, token, userID string, maxMembers int) (Group, int64, error)
}

// Sentinel errors the Repo may return for the join path.
var (
	ErrInviteInvalid = errors.New("groups: invite invalid")
	ErrGroupFull     = errors.New("groups: group full")
	ErrAlreadyMember = errors.New("groups: already a member")
)

// Events emits ordered group facts on group.events.{group_id}. Clients rotate
// Sender Keys on membership events; the fan-out cache tracks membership. Server
// never touches key material (e2ee-design §3).
type Events interface {
	MemberAdded(ctx context.Context, groupID string, version int64, actor, subject string)
	MemberRemoved(ctx context.Context, groupID string, version int64, actor, subject string)
	RoleChanged(ctx context.Context, groupID string, version int64, actor, subject string, role domain.Role)
	InfoChanged(ctx context.Context, groupID string, version int64, actor string)
	SettingsChanged(ctx context.Context, groupID string, version int64, actor string)
	GroupDeleted(ctx context.Context, groupID, actor string)
}

// Service orchestrates group management. All permission checks live here; the
// pure matrix is in domain/.
type Service struct {
	repo   Repo
	events Events
	now    func() time.Time
}

func NewService(repo Repo, events Events) *Service {
	return &Service{repo: repo, events: events, now: time.Now}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func notMember() error {
	return httpx.Reject(http.StatusForbidden, "STATE_NOT_MEMBER", "not a member of this group")
}

func forbidden() error {
	return httpx.Reject(http.StatusForbidden, "STATE_FORBIDDEN", "insufficient group permission")
}

// caller loads the requesting user's membership, mapping absence to a
// members-only rejection and infra failure to transient.
func (s *Service) caller(ctx context.Context, groupID, userID string) (Member, error) {
	m, err := s.repo.GetMember(ctx, groupID, userID)
	if errors.Is(err, ErrNotFound) {
		return Member{}, notMember()
	}
	if err != nil {
		return Member{}, httpx.Transient()
	}
	return m, nil
}

// ── operations ──────────────────────────────────────────────────────────────

// Create makes a group with the caller as owner and adds the given members
// (POST /v1/groups). Emits member_added per member.
func (s *Service) Create(ctx context.Context, ident auth.Identity, name, description string, memberIDs []string) (GroupView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return GroupView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_NAME", "group name required")
	}
	memberIDs = dedupeExcluding(memberIDs, ident.UserID)
	if 1+len(memberIDs) > domain.MaxMembers {
		return GroupView{}, httpx.Reject(http.StatusConflict, "STATE_GROUP_FULL", "group exceeds member cap")
	}

	now := s.now()
	g := Group{
		ID:          id.New(),
		Name:        name,
		Description: description,
		Settings:    domain.DefaultSettings(),
		Version:     0,
		CreatedBy:   ident.UserID,
		CreatedAt:   now,
	}
	members := make([]Member, 0, len(memberIDs)+1)
	members = append(members, Member{GroupID: g.ID, UserID: ident.UserID, Role: domain.RoleOwner, JoinedAt: now})
	for _, uid := range memberIDs {
		members = append(members, Member{GroupID: g.ID, UserID: uid, Role: domain.RoleMember, JoinedAt: now})
	}
	if err := s.repo.CreateGroup(ctx, g, members); err != nil {
		return GroupView{}, httpx.Transient()
	}
	for _, uid := range memberIDs {
		s.events.MemberAdded(ctx, g.ID, g.Version, ident.UserID, uid)
	}
	return g.view(domain.RoleOwner.String()), nil
}

// Get returns the group + the caller's role (GET /v1/groups/{id}), members-only.
func (s *Service) Get(ctx context.Context, ident auth.Identity, groupID string) (GroupView, error) {
	me, err := s.caller(ctx, groupID, ident.UserID)
	if err != nil {
		return GroupView{}, err
	}
	g, err := s.repo.GetGroup(ctx, groupID)
	if err != nil {
		return GroupView{}, s.mapGroupErr(err)
	}
	return g.view(me.Role.String()), nil
}

// UpdateInfo edits name/description/avatar (PATCH), gated by who_can_edit_info.
func (s *Service) UpdateInfo(ctx context.Context, ident auth.Identity, groupID string, name, description, avatarRef *string) error {
	me, err := s.caller(ctx, groupID, ident.UserID)
	if err != nil {
		return err
	}
	g, err := s.repo.GetGroup(ctx, groupID)
	if err != nil {
		return s.mapGroupErr(err)
	}
	if !domain.CanEditInfo(me.Role, g.Settings) {
		return forbidden()
	}
	if name != nil && strings.TrimSpace(*name) == "" {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_NAME", "group name cannot be empty")
	}
	version, err := s.repo.UpdateInfo(ctx, groupID, name, description, avatarRef)
	if err != nil {
		return s.mapGroupErr(err)
	}
	s.events.InfoChanged(ctx, groupID, version, ident.UserID)
	return nil
}

// SetSettings changes who_can_* / announcements (PUT /settings), admin+.
func (s *Service) SetSettings(ctx context.Context, ident auth.Identity, groupID string, st domain.Settings) error {
	me, err := s.caller(ctx, groupID, ident.UserID)
	if err != nil {
		return err
	}
	if !domain.CanEditSettings(me.Role) {
		return forbidden()
	}
	if !st.Valid() {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_SETTINGS", "who_can_* must be all|admins")
	}
	version, err := s.repo.UpdateSettings(ctx, groupID, st)
	if err != nil {
		return s.mapGroupErr(err)
	}
	s.events.SettingsChanged(ctx, groupID, version, ident.UserID)
	return nil
}

// ListMembers pages members (GET /members), members-only.
func (s *Service) ListMembers(ctx context.Context, ident auth.Identity, groupID, cursor string, limit int) ([]MemberView, string, error) {
	if _, err := s.caller(ctx, groupID, ident.UserID); err != nil {
		return nil, "", err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	members, err := s.repo.ListMembers(ctx, groupID, cursor, limit+1)
	if err != nil {
		return nil, "", httpx.Transient()
	}
	next := ""
	if len(members) > limit {
		next = members[limit-1].UserID
		members = members[:limit]
	}
	views := make([]MemberView, 0, len(members))
	for _, m := range members {
		views = append(views, MemberView{UserID: m.UserID, Role: m.Role.String()})
	}
	return views, next, nil
}

// AddMembers adds users (POST /members), admin+, enforces the cap.
func (s *Service) AddMembers(ctx context.Context, ident auth.Identity, groupID string, userIDs []string) ([]string, error) {
	me, err := s.caller(ctx, groupID, ident.UserID)
	if err != nil {
		return nil, err
	}
	if !domain.CanManageMembers(me.Role) {
		return nil, forbidden()
	}
	userIDs = dedupeExcluding(userIDs, "")
	if len(userIDs) == 0 {
		return []string{}, nil
	}
	count, err := s.repo.CountMembers(ctx, groupID)
	if err != nil {
		return nil, httpx.Transient()
	}
	if count+len(userIDs) > domain.MaxMembers {
		return nil, httpx.Reject(http.StatusConflict, "STATE_GROUP_FULL", "group exceeds member cap")
	}
	added, version, err := s.repo.AddMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, s.mapGroupErr(err)
	}
	for _, uid := range added {
		s.events.MemberAdded(ctx, groupID, version, ident.UserID, uid)
	}
	return added, nil
}

// RemoveMember removes a user (DELETE /members/{uid}), admin+ with role rules.
func (s *Service) RemoveMember(ctx context.Context, ident auth.Identity, groupID, targetID string) error {
	me, err := s.caller(ctx, groupID, ident.UserID)
	if err != nil {
		return err
	}
	if !domain.CanManageMembers(me.Role) {
		return forbidden()
	}
	target, err := s.repo.GetMember(ctx, groupID, targetID)
	if errors.Is(err, ErrNotFound) {
		return httpx.Reject(http.StatusNotFound, "MEMBER_NOT_FOUND", "no such member")
	}
	if err != nil {
		return httpx.Transient()
	}
	if !domain.CanRemove(me.Role, target.Role) {
		return forbidden()
	}
	version, removed, err := s.repo.RemoveMember(ctx, groupID, targetID)
	if err != nil {
		return s.mapGroupErr(err)
	}
	if removed {
		s.events.MemberRemoved(ctx, groupID, version, ident.UserID, targetID)
	}
	return nil
}

// SetRole promotes/demotes a member (PUT /members/{uid}/role), owner only.
func (s *Service) SetRole(ctx context.Context, ident auth.Identity, groupID, targetID string, role domain.Role) error {
	me, err := s.caller(ctx, groupID, ident.UserID)
	if err != nil {
		return err
	}
	if !domain.CanChangeRoles(me.Role) {
		return forbidden()
	}
	if !role.Assignable() {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_ROLE", "role must be member|admin")
	}
	if targetID == ident.UserID {
		return httpx.Reject(http.StatusConflict, "STATE_OWNER_SELF", "owner cannot change own role; transfer via leave")
	}
	if _, err := s.repo.GetMember(ctx, groupID, targetID); errors.Is(err, ErrNotFound) {
		return httpx.Reject(http.StatusNotFound, "MEMBER_NOT_FOUND", "no such member")
	} else if err != nil {
		return httpx.Transient()
	}
	version, err := s.repo.SetRole(ctx, groupID, targetID, role)
	if err != nil {
		return s.mapGroupErr(err)
	}
	s.events.RoleChanged(ctx, groupID, version, ident.UserID, targetID, role)
	return nil
}

// Leave removes the caller (POST /leave); the owner must transfer first.
func (s *Service) Leave(ctx context.Context, ident auth.Identity, groupID string) error {
	me, err := s.caller(ctx, groupID, ident.UserID)
	if err != nil {
		return err
	}
	if me.Role == domain.RoleOwner {
		return httpx.Reject(http.StatusConflict, "STATE_OWNER_MUST_TRANSFER", "transfer ownership before leaving")
	}
	version, removed, err := s.repo.RemoveMember(ctx, groupID, ident.UserID)
	if err != nil {
		return s.mapGroupErr(err)
	}
	if removed {
		s.events.MemberRemoved(ctx, groupID, version, ident.UserID, ident.UserID)
	}
	return nil
}

// Delete tombstones the group (DELETE /{id}), owner only.
func (s *Service) Delete(ctx context.Context, ident auth.Identity, groupID string) error {
	me, err := s.caller(ctx, groupID, ident.UserID)
	if err != nil {
		return err
	}
	if !domain.CanDeleteGroup(me.Role) {
		return forbidden()
	}
	if err := s.repo.DeleteGroup(ctx, groupID); err != nil {
		return s.mapGroupErr(err)
	}
	s.events.GroupDeleted(ctx, groupID, ident.UserID)
	return nil
}

// InviteResult is the POST /invite-links response.
type InviteResult struct {
	Token string `json:"token"`
	URL   string `json:"url"`
	QR    string `json:"qr"`
}

// CreateInviteLink mints a capability token (POST /invite-links), admin+.
func (s *Service) CreateInviteLink(ctx context.Context, ident auth.Identity, groupID string, expires *time.Time, maxUses *int) (InviteResult, error) {
	me, err := s.caller(ctx, groupID, ident.UserID)
	if err != nil {
		return InviteResult{}, err
	}
	if !domain.CanManageMembers(me.Role) {
		return InviteResult{}, forbidden()
	}
	token, err := newToken()
	if err != nil {
		return InviteResult{}, httpx.Transient()
	}
	l := InviteLink{Token: token, GroupID: groupID, CreatedBy: ident.UserID, ExpiresAt: expires, MaxUses: maxUses}
	if err := s.repo.CreateInvite(ctx, l); err != nil {
		return InviteResult{}, s.mapGroupErr(err)
	}
	url := "https://wa.invite/" + token
	return InviteResult{Token: token, URL: url, QR: url}, nil
}

// RevokeInviteLink revokes a token (DELETE /invite-links/{token}), admin+ on
// the token's group.
func (s *Service) RevokeInviteLink(ctx context.Context, ident auth.Identity, token string) error {
	l, err := s.repo.GetInvite(ctx, token)
	if errors.Is(err, ErrNotFound) {
		return httpx.Reject(http.StatusNotFound, "INVITE_NOT_FOUND", "no such invite")
	}
	if err != nil {
		return httpx.Transient()
	}
	me, err := s.caller(ctx, l.GroupID, ident.UserID)
	if err != nil {
		return err
	}
	if !domain.CanManageMembers(me.Role) {
		return forbidden()
	}
	if err := s.repo.RevokeInvite(ctx, token); err != nil {
		return s.mapGroupErr(err)
	}
	return nil
}

// Join consumes an invite and adds the caller (POST /join).
func (s *Service) Join(ctx context.Context, ident auth.Identity, token string) (GroupView, error) {
	g, version, err := s.repo.JoinViaInvite(ctx, token, ident.UserID, domain.MaxMembers)
	switch {
	case errors.Is(err, ErrInviteInvalid):
		return GroupView{}, httpx.Reject(http.StatusForbidden, "INVITE_INVALID", "invite expired, revoked, or exhausted")
	case errors.Is(err, ErrGroupFull):
		return GroupView{}, httpx.Reject(http.StatusConflict, "STATE_GROUP_FULL", "group exceeds member cap")
	case errors.Is(err, ErrAlreadyMember):
		// Idempotent: already in the group — return it without a new event.
		return g.view(domain.RoleMember.String()), nil
	case err != nil:
		return GroupView{}, httpx.Transient()
	}
	s.events.MemberAdded(ctx, g.ID, version, ident.UserID, ident.UserID)
	return g.view(domain.RoleMember.String()), nil
}

// ── internals ───────────────────────────────────────────────────────────────

func (s *Service) mapGroupErr(err error) error {
	if errors.Is(err, ErrNotFound) {
		return httpx.Reject(http.StatusNotFound, "GROUP_NOT_FOUND", "no such group")
	}
	return httpx.Transient()
}

func newToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// dedupeExcluding removes duplicates, empties, and (optionally) one user id.
func dedupeExcluding(ids []string, exclude string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, x := range ids {
		x = strings.TrimSpace(x)
		if x == "" || x == exclude {
			continue
		}
		if _, dup := seen[x]; dup {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}
