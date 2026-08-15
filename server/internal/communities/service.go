package communities

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/communities/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

const defaultDiscoverLimit = 50

// Service orchestrates community CRUD, membership/roles, group links, the shared
// calendar, moderation, and discovery.
type Service struct {
	store  Store
	groups GroupCreator
	now    func() time.Time
	newID  func() string
}

func NewService(store Store, groups GroupCreator) *Service {
	return &Service{store: store, groups: groups, now: time.Now, newID: id.New}
}

// Create makes a community, its announcement group (owner = caller), and the
// owner membership.
func (s *Service) Create(ctx context.Context, ident auth.Identity, name, description, kindStr string) (CreateResult, error) {
	kind, ok := parseKind(kindStr)
	if !ok {
		return CreateResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_KIND", "kind must be public or private")
	}
	if err := domain.ValidateCreate(name, description, kind); err != nil {
		return CreateResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_COMMUNITY", err.Error())
	}
	groupID, err := s.groups.CreateAnnouncementGroup(ctx, ident, strings.TrimSpace(name)+" — Announcements")
	if err != nil {
		return CreateResult{}, err // groups service returns a typed httpx error
	}
	c := Community{
		ID: s.newID(), Name: strings.TrimSpace(name), Description: description, Kind: kind,
		OwnerID: ident.UserID, AnnouncementGroupID: groupID, CreatedAt: s.now(),
	}
	if err := s.store.Create(ctx, c); err != nil {
		return CreateResult{}, httpx.Transient()
	}
	if err := s.store.AddMember(ctx, c.ID, ident.UserID, domain.RoleOwner); err != nil {
		return CreateResult{}, httpx.Transient()
	}
	return CreateResult{ID: c.ID, AnnouncementGroupID: groupID}, nil
}

// Get returns the caller-relative view. Private communities are visible only to
// members (else 404, so membership can't be probed).
func (s *Service) Get(ctx context.Context, ident auth.Identity, communityID string) (View, error) {
	c, err := s.load(ctx, communityID)
	if err != nil {
		return View{}, err
	}
	role, isMember := s.roleOf(ctx, communityID, ident.UserID)
	if c.Kind == domain.KindPrivate && !isMember {
		return View{}, notFound()
	}
	members, groups, err := s.store.Counts(ctx, communityID)
	if err != nil {
		return View{}, httpx.Transient()
	}
	v := View{
		ID: c.ID, Name: c.Name, Description: c.Description, Kind: c.Kind.String(),
		AnnouncementGroupID: c.AnnouncementGroupID, MemberCount: members, GroupCount: groups,
	}
	if isMember {
		v.MyRole = role.String()
	}
	return v, nil
}

// Join adds the caller to a public community (private is invite-only → 404).
func (s *Service) Join(ctx context.Context, ident auth.Identity, communityID string) error {
	c, err := s.load(ctx, communityID)
	if err != nil {
		return err
	}
	if c.Kind == domain.KindPrivate {
		return notFound()
	}
	if err := s.store.AddMember(ctx, communityID, ident.UserID, domain.RoleMember); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Leave removes the caller. The owner cannot leave (must delete or transfer).
func (s *Service) Leave(ctx context.Context, ident auth.Identity, communityID string) error {
	role, ok := s.roleOf(ctx, communityID, ident.UserID)
	if !ok {
		return nil // idempotent
	}
	if role == domain.RoleOwner {
		return httpx.Reject(http.StatusConflict, "STATE_OWNER_LEAVE", "the owner can't leave — delete the community instead")
	}
	if err := s.store.RemoveMember(ctx, communityID, ident.UserID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Members lists membership (members-only).
func (s *Service) Members(ctx context.Context, ident auth.Identity, communityID string) ([]Member, error) {
	if _, ok := s.roleOf(ctx, communityID, ident.UserID); !ok {
		return nil, notFound()
	}
	ms, err := s.store.ListMembers(ctx, communityID)
	if err != nil {
		return nil, httpx.Transient()
	}
	return ms, nil
}

// SetRole promotes/demotes a member (owner-only; target must be member/admin).
func (s *Service) SetRole(ctx context.Context, ident auth.Identity, communityID, targetUserID, roleStr string) error {
	if err := s.requireRole(ctx, communityID, ident.UserID, domain.CanChangeRoles); err != nil {
		return err
	}
	role, ok := parseRole(roleStr)
	if !ok || !role.Assignable() {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_ROLE", "role must be member or admin")
	}
	if targetUserID == ident.UserID {
		return httpx.Reject(http.StatusConflict, "STATE_SELF_ROLE", "can't change your own role")
	}
	if _, ok := s.roleOf(ctx, communityID, targetUserID); !ok {
		return notFound()
	}
	if err := s.store.SetRole(ctx, communityID, targetUserID, role); err != nil {
		return httpx.Transient()
	}
	return nil
}

// RemoveMember kicks a member (admin+; can't remove the owner or yourself).
func (s *Service) RemoveMember(ctx context.Context, ident auth.Identity, communityID, targetUserID string) error {
	caller, ok := s.roleOf(ctx, communityID, ident.UserID)
	if !ok || !domain.CanModerate(caller) {
		return notFound()
	}
	target, ok := s.roleOf(ctx, communityID, targetUserID)
	if !ok {
		return notFound()
	}
	if target == domain.RoleOwner || targetUserID == ident.UserID {
		return httpx.Reject(http.StatusConflict, "STATE_REMOVE", "can't remove the owner or yourself")
	}
	if err := s.store.RemoveMember(ctx, communityID, targetUserID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// AddGroup / RemoveGroup link member groups (admin+).
func (s *Service) AddGroup(ctx context.Context, ident auth.Identity, communityID, groupID string) error {
	if err := s.requireRole(ctx, communityID, ident.UserID, domain.CanManageGroups); err != nil {
		return err
	}
	if groupID == "" {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_GROUP", "group_id required")
	}
	if err := s.store.AddGroup(ctx, communityID, groupID); err != nil {
		return httpx.Transient()
	}
	return nil
}

func (s *Service) RemoveGroup(ctx context.Context, ident auth.Identity, communityID, groupID string) error {
	if err := s.requireRole(ctx, communityID, ident.UserID, domain.CanManageGroups); err != nil {
		return err
	}
	if err := s.store.RemoveGroup(ctx, communityID, groupID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Groups lists the community's linked group ids (members-only).
func (s *Service) Groups(ctx context.Context, ident auth.Identity, communityID string) ([]string, error) {
	if _, ok := s.roleOf(ctx, communityID, ident.UserID); !ok {
		return nil, notFound()
	}
	gs, err := s.store.ListGroups(ctx, communityID)
	if err != nil {
		return nil, httpx.Transient()
	}
	return gs, nil
}

// CreateEvent adds a calendar entry (admin+).
func (s *Service) CreateEvent(ctx context.Context, ident auth.Identity, communityID, title, description string, startsAtMS int64) (EventView, error) {
	if err := s.requireRole(ctx, communityID, ident.UserID, domain.CanModerate); err != nil {
		return EventView{}, err
	}
	startsAt := time.UnixMilli(startsAtMS)
	if startsAtMS == 0 {
		startsAt = time.Time{}
	}
	if err := domain.ValidateEvent(title, description, startsAt); err != nil {
		return EventView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_EVENT", err.Error())
	}
	e := Event{
		ID: s.newID(), CommunityID: communityID, Title: strings.TrimSpace(title), Description: description,
		StartsAt: startsAt, CreatedBy: ident.UserID, CreatedAt: s.now(),
	}
	if err := s.store.CreateEvent(ctx, e); err != nil {
		return EventView{}, httpx.Transient()
	}
	return eventView(e), nil
}

// Events lists upcoming + recent events (members-only). from defaults to now−1d.
func (s *Service) Events(ctx context.Context, ident auth.Identity, communityID string) ([]EventView, error) {
	if _, ok := s.roleOf(ctx, communityID, ident.UserID); !ok {
		return nil, notFound()
	}
	es, err := s.store.ListEvents(ctx, communityID, s.now().Add(-24*time.Hour))
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]EventView, len(es))
	for i, e := range es {
		out[i] = eventView(e)
	}
	return out, nil
}

// DeleteEvent removes an event (admin+).
func (s *Service) DeleteEvent(ctx context.Context, ident auth.Identity, communityID, eventID string) error {
	if err := s.requireRole(ctx, communityID, ident.UserID, domain.CanModerate); err != nil {
		return err
	}
	if err := s.store.DeleteEvent(ctx, communityID, eventID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Delete removes the whole community (owner-only). The announcement group is
// left intact (deleting it is a groups-context action).
func (s *Service) Delete(ctx context.Context, ident auth.Identity, communityID string) error {
	if err := s.requireRole(ctx, communityID, ident.UserID, domain.CanDelete); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, communityID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Discover / Search list public communities.
func (s *Service) Discover(ctx context.Context, limit int) ([]Summary, error) {
	out, err := s.store.Discover(ctx, clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	return out, nil
}

func (s *Service) Search(ctx context.Context, query string, limit int) ([]Summary, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []Summary{}, nil
	}
	out, err := s.store.Search(ctx, query, clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	return out, nil
}

// ── helpers ────────────────────────────────────────────────────────────────

func (s *Service) load(ctx context.Context, communityID string) (Community, error) {
	c, err := s.store.Get(ctx, communityID)
	if errors.Is(err, ErrNotFound) {
		return Community{}, notFound()
	}
	if err != nil {
		return Community{}, httpx.Transient()
	}
	return c, nil
}

func (s *Service) roleOf(ctx context.Context, communityID, userID string) (domain.Role, bool) {
	m, err := s.store.GetMember(ctx, communityID, userID)
	if err != nil {
		return domain.RoleMember, false
	}
	return m.Role, true
}

// requireRole loads the caller's role and applies a permission gate; a
// non-member or under-privileged caller gets 404 (can't probe the community).
func (s *Service) requireRole(ctx context.Context, communityID, userID string, gate func(domain.Role) bool) error {
	if _, err := s.load(ctx, communityID); err != nil {
		return err
	}
	role, ok := s.roleOf(ctx, communityID, userID)
	if !ok || !gate(role) {
		return notFound()
	}
	return nil
}

func eventView(e Event) EventView {
	return EventView{ID: e.ID, Title: e.Title, Description: e.Description, StartsAtMS: e.StartsAt.UnixMilli(), CreatedBy: e.CreatedBy}
}

func notFound() error {
	return httpx.Reject(http.StatusNotFound, "COMMUNITY_NOT_FOUND", "community not found")
}

func clampLimit(n int) int {
	if n <= 0 || n > defaultDiscoverLimit {
		return defaultDiscoverLimit
	}
	return n
}

func parseKind(s string) (domain.Kind, bool) {
	switch s {
	case "public", "":
		return domain.KindPublic, true
	case "private":
		return domain.KindPrivate, true
	default:
		return 0, false
	}
}

func parseRole(s string) (domain.Role, bool) {
	switch s {
	case "member":
		return domain.RoleMember, true
	case "admin":
		return domain.RoleAdmin, true
	case "owner":
		return domain.RoleOwner, true
	default:
		return 0, false
	}
}
