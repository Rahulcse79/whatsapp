package groups

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/groups/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── in-memory Repo + Events fakes ───────────────────────────────────────────

type memRepo struct {
	groups  map[string]Group
	members map[string]map[string]Member // groupID → userID → member
	invites map[string]InviteLink
	now     func() time.Time
}

func newMemRepo() *memRepo {
	return &memRepo{
		groups:  map[string]Group{},
		members: map[string]map[string]Member{},
		invites: map[string]InviteLink{},
		now:     time.Now,
	}
}

func (r *memRepo) CreateGroup(_ context.Context, g Group, members []Member) error {
	r.groups[g.ID] = g
	r.members[g.ID] = map[string]Member{}
	for _, m := range members {
		r.members[g.ID][m.UserID] = m
	}
	return nil
}
func (r *memRepo) GetGroup(_ context.Context, id string) (Group, error) {
	g, ok := r.groups[id]
	if !ok {
		return Group{}, ErrNotFound
	}
	return g, nil
}
func (r *memRepo) GetMember(_ context.Context, gid, uid string) (Member, error) {
	m, ok := r.members[gid][uid]
	if !ok {
		return Member{}, ErrNotFound
	}
	return m, nil
}
func (r *memRepo) ListMembers(_ context.Context, gid, after string, limit int) ([]Member, error) {
	var out []Member
	for _, m := range r.members[gid] {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	filtered := out[:0]
	for _, m := range out {
		if after == "" || m.UserID > after {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}
func (r *memRepo) CountMembers(_ context.Context, gid string) (int, error) {
	return len(r.members[gid]), nil
}
func (r *memRepo) bump(gid string) int64 {
	g := r.groups[gid]
	g.Version++
	r.groups[gid] = g
	return g.Version
}
func (r *memRepo) UpdateInfo(_ context.Context, gid string, name, description, avatar *string) (int64, error) {
	g, ok := r.groups[gid]
	if !ok {
		return 0, ErrNotFound
	}
	if name != nil {
		g.Name = *name
	}
	if description != nil {
		g.Description = *description
	}
	if avatar != nil {
		g.AvatarRef = *avatar
	}
	r.groups[gid] = g
	return r.bump(gid), nil
}
func (r *memRepo) UpdateSettings(_ context.Context, gid string, s domain.Settings) (int64, error) {
	g, ok := r.groups[gid]
	if !ok {
		return 0, ErrNotFound
	}
	g.Settings = s
	r.groups[gid] = g
	return r.bump(gid), nil
}
func (r *memRepo) AddMembers(_ context.Context, gid string, userIDs []string) ([]string, int64, error) {
	if _, ok := r.groups[gid]; !ok {
		return nil, 0, ErrNotFound
	}
	var added []string
	for _, uid := range userIDs {
		if _, exists := r.members[gid][uid]; exists {
			continue
		}
		r.members[gid][uid] = Member{GroupID: gid, UserID: uid, Role: domain.RoleMember, JoinedAt: r.now()}
		added = append(added, uid)
	}
	return added, r.bump(gid), nil
}
func (r *memRepo) RemoveMember(_ context.Context, gid, uid string) (int64, bool, error) {
	if _, ok := r.members[gid][uid]; !ok {
		return 0, false, nil
	}
	delete(r.members[gid], uid)
	return r.bump(gid), true, nil
}
func (r *memRepo) SetRole(_ context.Context, gid, uid string, role domain.Role) (int64, error) {
	m, ok := r.members[gid][uid]
	if !ok {
		return 0, ErrNotFound
	}
	m.Role = role
	r.members[gid][uid] = m
	return r.bump(gid), nil
}
func (r *memRepo) DeleteGroup(_ context.Context, gid string) error {
	if _, ok := r.groups[gid]; !ok {
		return ErrNotFound
	}
	delete(r.groups, gid)
	delete(r.members, gid)
	return nil
}
func (r *memRepo) CreateInvite(_ context.Context, l InviteLink) error {
	r.invites[l.Token] = l
	return nil
}
func (r *memRepo) GetInvite(_ context.Context, token string) (InviteLink, error) {
	l, ok := r.invites[token]
	if !ok {
		return InviteLink{}, ErrNotFound
	}
	return l, nil
}
func (r *memRepo) RevokeInvite(_ context.Context, token string) error {
	l, ok := r.invites[token]
	if !ok {
		return ErrNotFound
	}
	now := r.now()
	l.RevokedAt = &now
	r.invites[token] = l
	return nil
}
func (r *memRepo) JoinViaInvite(_ context.Context, token, uid string, maxMembers int) (Group, int64, error) {
	l, ok := r.invites[token]
	if !ok || l.RevokedAt != nil {
		return Group{}, 0, ErrInviteInvalid
	}
	if l.ExpiresAt != nil && r.now().After(*l.ExpiresAt) {
		return Group{}, 0, ErrInviteInvalid
	}
	if l.MaxUses != nil && l.Uses >= *l.MaxUses {
		return Group{}, 0, ErrInviteInvalid
	}
	g, ok := r.groups[l.GroupID]
	if !ok {
		return Group{}, 0, ErrInviteInvalid
	}
	if _, member := r.members[l.GroupID][uid]; member {
		return g, g.Version, ErrAlreadyMember
	}
	if len(r.members[l.GroupID]) >= maxMembers {
		return g, 0, ErrGroupFull
	}
	r.members[l.GroupID][uid] = Member{GroupID: l.GroupID, UserID: uid, Role: domain.RoleMember, JoinedAt: r.now()}
	l.Uses++
	r.invites[token] = l
	return g, r.bump(l.GroupID), nil
}

type recEvents struct {
	added, removed []string
	roleChanged    int
	settings, info int
	deleted        int
}

func (e *recEvents) MemberAdded(_ context.Context, _ string, _ int64, _, subject string) {
	e.added = append(e.added, subject)
}
func (e *recEvents) MemberRemoved(_ context.Context, _ string, _ int64, _, subject string) {
	e.removed = append(e.removed, subject)
}
func (e *recEvents) RoleChanged(_ context.Context, _ string, _ int64, _, _ string, _ domain.Role) {
	e.roleChanged++
}
func (e *recEvents) InfoChanged(_ context.Context, _ string, _ int64, _ string)     { e.info++ }
func (e *recEvents) SettingsChanged(_ context.Context, _ string, _ int64, _ string) { e.settings++ }
func (e *recEvents) GroupDeleted(_ context.Context, _, _ string)                    { e.deleted++ }

// ── harness ─────────────────────────────────────────────────────────────────

type harness struct {
	svc    *Service
	repo   *memRepo
	events *recEvents
}

func newHarness() *harness {
	repo, events := newMemRepo(), &recEvents{}
	return &harness{svc: NewService(repo, events), repo: repo, events: events}
}

func ident(u string) auth.Identity { return auth.Identity{UserID: u, DeviceID: "d1", SessionID: "s1"} }

func code(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

// createGroup is a test helper: owner "o1" creates a group with the given members.
func (h *harness) create(t *testing.T, owner string, members ...string) GroupView {
	t.Helper()
	g, err := h.svc.Create(context.Background(), ident(owner), "Team", "", members)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return g
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestCreate_OwnerAndMembers(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1", "a", "b")
	if g.MyRole != "owner" {
		t.Fatalf("creator role = %s, want owner", g.MyRole)
	}
	if n, _ := h.repo.CountMembers(context.Background(), g.ID); n != 3 {
		t.Fatalf("member count = %d, want 3 (owner + 2)", n)
	}
	if len(h.events.added) != 2 {
		t.Fatalf("member_added events = %d, want 2", len(h.events.added))
	}
}

func TestCreate_Validation(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Create(context.Background(), ident("o1"), "  ", "", nil); code(t, err) != "VALIDATION_NAME" {
		t.Fatal("empty name accepted")
	}
	many := make([]string, domain.MaxMembers) // + owner overflows the cap
	for i := range many {
		many[i] = "user-" + strconv.Itoa(i)
	}
	if _, err := h.svc.Create(context.Background(), ident("o1"), "Big", "", many); code(t, err) != "STATE_GROUP_FULL" {
		t.Fatal("over-cap create accepted")
	}
}

func TestGet_MembersOnly(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1", "a")
	if _, err := h.svc.Get(context.Background(), ident("stranger"), g.ID); code(t, err) != "STATE_NOT_MEMBER" {
		t.Fatal("non-member read allowed")
	}
	view, err := h.svc.Get(context.Background(), ident("a"), g.ID)
	if err != nil || view.MyRole != "member" {
		t.Fatalf("member read wrong: %+v err=%v", view, err)
	}
}

func TestUpdateInfo_Permission(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1", "a")
	name := "Renamed"
	// default who_can_edit_info=admins → plain member can't
	if err := h.svc.UpdateInfo(context.Background(), ident("a"), g.ID, &name, nil, nil); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("member edited info under admins policy")
	}
	if err := h.svc.UpdateInfo(context.Background(), ident("o1"), g.ID, &name, nil, nil); err != nil {
		t.Fatalf("owner edit failed: %v", err)
	}
	if h.events.info != 1 {
		t.Fatal("info_changed not emitted")
	}
}

func TestSettings_AdminOnlyAndValidated(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1", "a")
	bad := domain.Settings{WhoCanPost: "nope", WhoCanEditInfo: domain.PolicyAll}
	if err := h.svc.SetSettings(context.Background(), ident("o1"), g.ID, bad); code(t, err) != "VALIDATION_SETTINGS" {
		t.Fatal("invalid settings accepted")
	}
	ok := domain.Settings{WhoCanPost: domain.PolicyAdmins, WhoCanEditInfo: domain.PolicyAll}
	if err := h.svc.SetSettings(context.Background(), ident("a"), g.ID, ok); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("member changed settings")
	}
	if err := h.svc.SetSettings(context.Background(), ident("o1"), g.ID, ok); err != nil {
		t.Fatalf("owner settings failed: %v", err)
	}
}

func TestAddMembers_PermissionAndEvents(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1", "a")
	if _, err := h.svc.AddMembers(context.Background(), ident("a"), g.ID, []string{"b"}); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("member added members")
	}
	added, err := h.svc.AddMembers(context.Background(), ident("o1"), g.ID, []string{"b", "c", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("added %v, want b,c (deduped)", added)
	}
}

func TestRemoveMember_RoleRules(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1", "a", "b")
	// promote a to admin
	if err := h.svc.SetRole(context.Background(), ident("o1"), g.ID, "a", domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// admin a cannot remove admin... make b admin too
	if err := h.svc.SetRole(context.Background(), ident("o1"), g.ID, "b", domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.RemoveMember(context.Background(), ident("a"), g.ID, "b"); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("admin removed another admin")
	}
	// owner removes admin b
	if err := h.svc.RemoveMember(context.Background(), ident("o1"), g.ID, "b"); err != nil {
		t.Fatalf("owner remove admin failed: %v", err)
	}
	if got := h.events.removed; len(got) != 1 || got[0] != "b" {
		t.Fatalf("member_removed events = %v", got)
	}
}

func TestSetRole_OwnerOnly(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1", "a", "b")
	if err := h.svc.SetRole(context.Background(), ident("a"), g.ID, "b", domain.RoleAdmin); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("non-owner changed a role")
	}
	if err := h.svc.SetRole(context.Background(), ident("o1"), g.ID, "b", domain.RoleOwner); code(t, err) != "VALIDATION_ROLE" {
		t.Fatal("assigned owner via set-role")
	}
	if err := h.svc.SetRole(context.Background(), ident("o1"), g.ID, "b", domain.RoleAdmin); err != nil {
		t.Fatalf("owner promote failed: %v", err)
	}
}

func TestLeave_OwnerMustTransfer(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1", "a")
	if err := h.svc.Leave(context.Background(), ident("o1"), g.ID); code(t, err) != "STATE_OWNER_MUST_TRANSFER" {
		t.Fatal("owner left without transfer")
	}
	if err := h.svc.Leave(context.Background(), ident("a"), g.ID); err != nil {
		t.Fatalf("member leave failed: %v", err)
	}
}

func TestDelete_OwnerOnly(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1", "a")
	if err := h.svc.Delete(context.Background(), ident("a"), g.ID); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("member deleted group")
	}
	if err := h.svc.Delete(context.Background(), ident("o1"), g.ID); err != nil {
		t.Fatalf("owner delete failed: %v", err)
	}
	if h.events.deleted != 1 {
		t.Fatal("group_deleted not emitted")
	}
}

func TestInviteAndJoin(t *testing.T) {
	h := newHarness()
	g := h.create(t, "o1")
	inv, err := h.svc.CreateInviteLink(context.Background(), ident("o1"), g.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined, err := h.svc.Join(context.Background(), ident("x"), inv.Token)
	if err != nil {
		t.Fatalf("join failed: %v", err)
	}
	if joined.ID != g.ID || joined.MyRole != "member" {
		t.Fatalf("join view wrong: %+v", joined)
	}
	if last := h.events.added; len(last) == 0 || last[len(last)-1] != "x" {
		t.Fatalf("join did not emit member_added: %v", last)
	}
	// second join = idempotent, no new event
	before := len(h.events.added)
	if _, err := h.svc.Join(context.Background(), ident("x"), inv.Token); err != nil {
		t.Fatalf("re-join errored: %v", err)
	}
	if len(h.events.added) != before {
		t.Fatal("idempotent re-join emitted a duplicate event")
	}
	// revoked invite rejected
	if err := h.svc.RevokeInviteLink(context.Background(), ident("o1"), inv.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Join(context.Background(), ident("y"), inv.Token); code(t, err) != "INVITE_INVALID" {
		t.Fatal("revoked invite accepted")
	}
}
