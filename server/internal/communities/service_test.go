package communities

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/communities/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeStore struct {
	byID    map[string]Community
	members map[string]map[string]domain.Role // communityID → userID → role
	groups  map[string]map[string]bool        // communityID → groupID set
	events  map[string]map[string]Event       // communityID → eventID → event
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byID:    map[string]Community{},
		members: map[string]map[string]domain.Role{},
		groups:  map[string]map[string]bool{},
		events:  map[string]map[string]Event{},
	}
}
func (s *fakeStore) Create(_ context.Context, c Community) error { s.byID[c.ID] = c; return nil }
func (s *fakeStore) Get(_ context.Context, id string) (Community, error) {
	c, ok := s.byID[id]
	if !ok {
		return Community{}, ErrNotFound
	}
	return c, nil
}
func (s *fakeStore) Delete(_ context.Context, id string) error { delete(s.byID, id); return nil }
func (s *fakeStore) Counts(_ context.Context, id string) (int, int, error) {
	return len(s.members[id]), len(s.groups[id]), nil
}
func (s *fakeStore) AddMember(_ context.Context, cid, uid string, role domain.Role) error {
	if s.members[cid] == nil {
		s.members[cid] = map[string]domain.Role{}
	}
	if _, ok := s.members[cid][uid]; !ok {
		s.members[cid][uid] = role
	}
	return nil
}
func (s *fakeStore) RemoveMember(_ context.Context, cid, uid string) error {
	delete(s.members[cid], uid)
	return nil
}
func (s *fakeStore) GetMember(_ context.Context, cid, uid string) (Member, error) {
	r, ok := s.members[cid][uid]
	if !ok {
		return Member{}, ErrNotFound
	}
	return Member{UserID: uid, Role: r}, nil
}
func (s *fakeStore) ListMembers(_ context.Context, cid string) ([]Member, error) {
	var out []Member
	for u, r := range s.members[cid] {
		out = append(out, Member{UserID: u, Role: r})
	}
	return out, nil
}
func (s *fakeStore) SetRole(_ context.Context, cid, uid string, role domain.Role) error {
	s.members[cid][uid] = role
	return nil
}
func (s *fakeStore) AddGroup(_ context.Context, cid, gid string) error {
	if s.groups[cid] == nil {
		s.groups[cid] = map[string]bool{}
	}
	s.groups[cid][gid] = true
	return nil
}
func (s *fakeStore) RemoveGroup(_ context.Context, cid, gid string) error {
	delete(s.groups[cid], gid)
	return nil
}
func (s *fakeStore) ListGroups(_ context.Context, cid string) ([]string, error) {
	var out []string
	for g := range s.groups[cid] {
		out = append(out, g)
	}
	return out, nil
}
func (s *fakeStore) CreateEvent(_ context.Context, e Event) error {
	if s.events[e.CommunityID] == nil {
		s.events[e.CommunityID] = map[string]Event{}
	}
	s.events[e.CommunityID][e.ID] = e
	return nil
}
func (s *fakeStore) ListEvents(_ context.Context, cid string, _ time.Time) ([]Event, error) {
	var out []Event
	for _, e := range s.events[cid] {
		out = append(out, e)
	}
	return out, nil
}
func (s *fakeStore) DeleteEvent(_ context.Context, cid, eid string) error {
	delete(s.events[cid], eid)
	return nil
}
func (s *fakeStore) Discover(_ context.Context, _ int) ([]Summary, error) {
	var out []Summary
	for _, c := range s.byID {
		if c.Kind == domain.KindPublic {
			out = append(out, Summary{ID: c.ID, Name: c.Name})
		}
	}
	return out, nil
}
func (s *fakeStore) Search(_ context.Context, _ string, _ int) ([]Summary, error) {
	return s.Discover(context.Background(), 0)
}

type fakeGroups struct{ n int }

func (g *fakeGroups) CreateAnnouncementGroup(_ context.Context, _ auth.Identity, _ string) (string, error) {
	g.n++
	return "group-ann", nil
}

// ── helpers ──────────────────────────────────────────────────────────────

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func newSvc() (*Service, *fakeStore, *fakeGroups) {
	store := newFakeStore()
	groups := &fakeGroups{}
	svc := NewService(store, groups)
	n := 0
	svc.newID = func() string { n++; return fmt.Sprintf("id%d", n) }
	svc.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return svc, store, groups
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

// ── tests ──────────────────────────────────────────────────────────────────

func TestCreateMakesAnnouncementGroupAndOwner(t *testing.T) {
	svc, store, groups := newSvc()
	res, err := svc.Create(context.Background(), who("u1"), "Devs", "hi", "public")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.AnnouncementGroupID != "group-ann" || groups.n != 1 {
		t.Fatalf("announcement group not created: %+v", res)
	}
	if store.members[res.ID]["u1"] != domain.RoleOwner {
		t.Fatalf("creator should be owner")
	}
	if _, err := svc.Create(context.Background(), who("u1"), "", "x", "public"); codeOf(t, err) != "VALIDATION_COMMUNITY" {
		t.Fatalf("empty name rejected")
	}
}

func TestJoinRespectsVisibility(t *testing.T) {
	svc, _, _ := newSvc()
	pub, _ := svc.Create(context.Background(), who("u1"), "Pub", "", "public")
	if err := svc.Join(context.Background(), who("u2"), pub.ID); err != nil {
		t.Fatalf("public join: %v", err)
	}
	priv, _ := svc.Create(context.Background(), who("u1"), "Priv", "", "private")
	if err := svc.Join(context.Background(), who("u2"), priv.ID); codeOf(t, err) != "COMMUNITY_NOT_FOUND" {
		t.Fatalf("private join should 404")
	}
	// non-member sees a private community as 404
	if _, err := svc.Get(context.Background(), who("u3"), priv.ID); codeOf(t, err) != "COMMUNITY_NOT_FOUND" {
		t.Fatalf("private get by non-member should 404")
	}
}

func TestOwnerCannotLeave(t *testing.T) {
	svc, _, _ := newSvc()
	c, _ := svc.Create(context.Background(), who("u1"), "C", "", "public")
	if err := svc.Leave(context.Background(), who("u1"), c.ID); codeOf(t, err) != "STATE_OWNER_LEAVE" {
		t.Fatalf("owner leave should be rejected")
	}
	_ = svc.Join(context.Background(), who("u2"), c.ID)
	if err := svc.Leave(context.Background(), who("u2"), c.ID); err != nil {
		t.Fatalf("member leave: %v", err)
	}
}

func TestRolesAndModeration(t *testing.T) {
	svc, store, _ := newSvc()
	c, _ := svc.Create(context.Background(), who("u1"), "C", "", "public")
	_ = svc.Join(context.Background(), who("u2"), c.ID)
	_ = svc.Join(context.Background(), who("u3"), c.ID)

	// non-owner can't change roles
	if err := svc.SetRole(context.Background(), who("u2"), c.ID, "u3", "admin"); codeOf(t, err) != "COMMUNITY_NOT_FOUND" {
		t.Fatalf("member set-role should 404")
	}
	// owner promotes u2 → admin
	if err := svc.SetRole(context.Background(), who("u1"), c.ID, "u2", "admin"); err != nil {
		t.Fatalf("owner set-role: %v", err)
	}
	if store.members[c.ID]["u2"] != domain.RoleAdmin {
		t.Fatalf("u2 should be admin")
	}
	// admin u2 removes member u3
	if err := svc.RemoveMember(context.Background(), who("u2"), c.ID, "u3"); err != nil {
		t.Fatalf("admin remove: %v", err)
	}
	if _, ok := store.members[c.ID]["u3"]; ok {
		t.Fatalf("u3 should be removed")
	}
	// nobody can remove the owner
	if err := svc.RemoveMember(context.Background(), who("u2"), c.ID, "u1"); codeOf(t, err) != "STATE_REMOVE" {
		t.Fatalf("removing owner should be rejected")
	}
}

func TestGroupsAndEvents(t *testing.T) {
	svc, _, _ := newSvc()
	c, _ := svc.Create(context.Background(), who("u1"), "C", "", "public")
	_ = svc.Join(context.Background(), who("u2"), c.ID)

	// member can't link a group
	if err := svc.AddGroup(context.Background(), who("u2"), c.ID, "g1"); codeOf(t, err) != "COMMUNITY_NOT_FOUND" {
		t.Fatalf("member add-group should 404")
	}
	if err := svc.AddGroup(context.Background(), who("u1"), c.ID, "g1"); err != nil {
		t.Fatalf("owner add-group: %v", err)
	}
	if gs, _ := svc.Groups(context.Background(), who("u1"), c.ID); len(gs) != 1 || gs[0] != "g1" {
		t.Fatalf("group not linked: %v", gs)
	}

	// events: admin+ create; validation on empty title
	if _, err := svc.CreateEvent(context.Background(), who("u1"), c.ID, "", "", 5_000); codeOf(t, err) != "VALIDATION_EVENT" {
		t.Fatalf("empty event title rejected")
	}
	ev, err := svc.CreateEvent(context.Background(), who("u1"), c.ID, "Standup", "daily", 5_000)
	if err != nil || ev.Title != "Standup" || ev.StartsAtMS != 5_000 {
		t.Fatalf("create event: %v %+v", err, ev)
	}
	if err := svc.DeleteEvent(context.Background(), who("u1"), c.ID, ev.ID); err != nil {
		t.Fatalf("delete event: %v", err)
	}
}

func TestDiscover(t *testing.T) {
	svc, _, _ := newSvc()
	_, _ = svc.Create(context.Background(), who("u1"), "Public C", "", "public")
	_, _ = svc.Create(context.Background(), who("u1"), "Private C", "", "private")
	out, err := svc.Discover(context.Background(), 0)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(out) != 1 || out[0].Name != "Public C" {
		t.Fatalf("discover should list only public: %+v", out)
	}
}
