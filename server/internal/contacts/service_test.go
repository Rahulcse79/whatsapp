package contacts

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/contacts/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fakes ───────────────────────────────────────────────────────────────────

// fakeHasher produces a distinct, deterministic hash per handle so tests can map
// a match back to the handle that produced it (mirrors the real peppered HMAC's
// one-hash-per-handle property without any crypto).
type fakeHasher struct{}

func (fakeHasher) PhoneHash(h string) []byte { return []byte("H:" + h) }

type fakeDirectory struct {
	byHash map[string]Match // string(peppered-hash) → match
	search []UserRef
	err    error
}

func (d *fakeDirectory) MatchHashes(_ context.Context, hashes [][]byte) ([]Match, error) {
	if d.err != nil {
		return nil, d.err
	}
	var out []Match
	for _, h := range hashes {
		if m, ok := d.byHash[string(h)]; ok {
			m.Hash = h // echo the exact queried bytes, like the pg adapter
			out = append(out, m)
		}
	}
	return out, nil
}

func (d *fakeDirectory) SearchUsername(_ context.Context, _ string, limit int) ([]UserRef, error) {
	if d.err != nil {
		return nil, d.err
	}
	if len(d.search) > limit {
		return d.search[:limit], nil
	}
	return d.search, nil
}

type fakeContactStore struct {
	saved map[string][]ContactEdge
	err   error
}

func (s *fakeContactStore) UpsertMatched(_ context.Context, owner string, edges []ContactEdge) error {
	if s.err != nil {
		return s.err
	}
	if s.saved == nil {
		s.saved = map[string][]ContactEdge{}
	}
	s.saved[owner] = append(s.saved[owner], edges...)
	return nil
}

type fakeFavorites struct {
	known map[string]bool            // valid target user ids
	set   map[string]map[string]bool // owner → target → present
	list  []UserRef
}

func (f *fakeFavorites) Add(_ context.Context, owner, target string) error {
	if !f.known[target] {
		return ErrNotFound
	}
	if f.set == nil {
		f.set = map[string]map[string]bool{}
	}
	if f.set[owner] == nil {
		f.set[owner] = map[string]bool{}
	}
	f.set[owner][target] = true
	return nil
}

func (f *fakeFavorites) Remove(_ context.Context, owner, target string) error {
	if f.set[owner] != nil {
		delete(f.set[owner], target)
	}
	return nil
}

func (f *fakeFavorites) List(_ context.Context, _ string) ([]UserRef, error) { return f.list, nil }

type fakeInvites struct {
	byToken map[string]domain.Invite
}

func (i *fakeInvites) Create(_ context.Context, inv domain.Invite) error {
	if i.byToken == nil {
		i.byToken = map[string]domain.Invite{}
	}
	i.byToken[inv.Token] = inv
	return nil
}

func (i *fakeInvites) Get(_ context.Context, token string) (domain.Invite, error) {
	inv, ok := i.byToken[token]
	if !ok {
		return domain.Invite{}, ErrNotFound
	}
	return inv, nil
}

func (i *fakeInvites) Revoke(_ context.Context, inviter, token string) error {
	inv, ok := i.byToken[token]
	if !ok || inv.InviterID != inviter {
		return ErrNotFound
	}
	now := time.Now()
	inv.RevokedAt = &now
	i.byToken[token] = inv
	return nil
}

type fakeDaily struct {
	used int
	deny bool
	err  error
}

func (d *fakeDaily) AllowDaily(_ context.Context, _ string, limit int) (bool, error) {
	if d.err != nil {
		return false, d.err
	}
	if d.deny {
		return false, nil
	}
	d.used++
	return d.used <= limit, nil
}

type fakeRate struct {
	deny bool
	err  error
}

func (r *fakeRate) Allow(_ context.Context, _ string) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	return !r.deny, nil
}

// ── harness ─────────────────────────────────────────────────────────────────

type deps struct {
	dir   *fakeDirectory
	store *fakeContactStore
	fav   *fakeFavorites
	inv   *fakeInvites
	daily *fakeDaily
	rate  *fakeRate
}

func buildSvc() (*Service, *deps) {
	d := &deps{
		dir:   &fakeDirectory{byHash: map[string]Match{}},
		store: &fakeContactStore{},
		fav:   &fakeFavorites{known: map[string]bool{}},
		inv:   &fakeInvites{byToken: map[string]domain.Invite{}},
		daily: &fakeDaily{},
		rate:  &fakeRate{},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewService(fakeHasher{}, d.dir, d.store, d.fav, d.inv, d.daily, d.rate, "https://wa.test", log)
	return s, d
}

func assertAPIErr(t *testing.T, err error, status int, code string) {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %v", err)
	}
	if ae.Status != status || ae.Code != code {
		t.Fatalf("got (%d,%s), want (%d,%s)", ae.Status, ae.Code, status, code)
	}
}

// ── Sync ────────────────────────────────────────────────────────────────────

func TestSync_MatchesEchoesAndPersists(t *testing.T) {
	s, d := buildSvc()
	ident := auth.Identity{UserID: "owner1"}
	h1, h2, h3 := "+14155550001", "+14155550002", "+14155550003"
	d.dir.byHash[string([]byte("H:"+h1))] = Match{UserID: "u1", Username: "alice"}
	d.dir.byHash[string([]byte("H:"+h2))] = Match{UserID: "u2", Username: "bob"}

	// h1 appears twice (dedup) and one handle is malformed (skipped, no match).
	out, err := s.Sync(context.Background(), ident, []string{h1, h2, h3, "garbage", h1})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d matches, want 2", len(out))
	}
	byUser := map[string]MatchedContact{}
	for _, m := range out {
		byUser[m.UserID] = m
	}
	if byUser["u1"].Handle != h1 || byUser["u1"].Username != "alice" {
		t.Errorf("u1 mapping wrong: %+v", byUser["u1"])
	}
	if byUser["u2"].Handle != h2 {
		t.Errorf("u2 handle not echoed: %+v", byUser["u2"])
	}
	if got := len(d.store.saved["owner1"]); got != 2 {
		t.Errorf("persisted %d edges, want 2", got)
	}
}

func TestSync_Empty(t *testing.T) {
	s, _ := buildSvc()
	_, err := s.Sync(context.Background(), auth.Identity{UserID: "o"}, nil)
	assertAPIErr(t, err, 400, "VALIDATION_EMPTY")
}

func TestSync_TooMany(t *testing.T) {
	s, _ := buildSvc()
	handles := make([]string, domain.MaxSyncHandles+1)
	_, err := s.Sync(context.Background(), auth.Identity{UserID: "o"}, handles)
	assertAPIErr(t, err, 413, "VALIDATION_TOO_MANY")
}

func TestSync_DailyLimited(t *testing.T) {
	s, d := buildSvc()
	d.daily.deny = true
	_, err := s.Sync(context.Background(), auth.Identity{UserID: "o"}, []string{"+14155550001"})
	assertAPIErr(t, err, 429, "RATE_LIMITED")
}

func TestSync_PersistFailureDoesNotFailSync(t *testing.T) {
	s, d := buildSvc()
	d.store.err = errors.New("db down")
	d.dir.byHash[string([]byte("H:+14155550001"))] = Match{UserID: "u1", Username: "alice"}
	out, err := s.Sync(context.Background(), auth.Identity{UserID: "o"}, []string{"+14155550001"})
	if err != nil {
		t.Fatalf("a persistence hiccup must not fail a sync the client already sees: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d matches, want 1", len(out))
	}
}

// ── Search ──────────────────────────────────────────────────────────────────

func TestSearch_ShortQuery(t *testing.T) {
	s, _ := buildSvc()
	_, err := s.Search(context.Background(), auth.Identity{UserID: "o"}, "a")
	assertAPIErr(t, err, 400, "VALIDATION_QUERY")
}

func TestSearch_RateLimited(t *testing.T) {
	s, d := buildSvc()
	d.rate.deny = true
	_, err := s.Search(context.Background(), auth.Identity{UserID: "o"}, "alice")
	assertAPIErr(t, err, 429, "RATE_LIMITED")
}

func TestSearch_ExcludesCaller(t *testing.T) {
	s, d := buildSvc()
	d.dir.search = []UserRef{{UserID: "owner1", Username: "me"}, {UserID: "u2", Username: "bob"}}
	res, err := s.Search(context.Background(), auth.Identity{UserID: "owner1"}, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].UserID != "u2" {
		t.Fatalf("caller must be excluded; got %+v", res)
	}
}

// ── Favorites ───────────────────────────────────────────────────────────────

func TestAddFavorite_SelfRejected(t *testing.T) {
	s, _ := buildSvc()
	err := s.AddFavorite(context.Background(), auth.Identity{UserID: "u1"}, "u1")
	assertAPIErr(t, err, 400, "VALIDATION_TARGET")
}

func TestAddFavorite_UnknownTarget(t *testing.T) {
	s, _ := buildSvc()
	err := s.AddFavorite(context.Background(), auth.Identity{UserID: "u1"}, "ghost")
	assertAPIErr(t, err, 404, "USER_NOT_FOUND")
}

func TestAddFavorite_OK(t *testing.T) {
	s, d := buildSvc()
	d.fav.known["u2"] = true
	if err := s.AddFavorite(context.Background(), auth.Identity{UserID: "u1"}, "u2"); err != nil {
		t.Fatal(err)
	}
	if !d.fav.set["u1"]["u2"] {
		t.Error("favorite not recorded")
	}
}

func TestListFavorites(t *testing.T) {
	s, d := buildSvc()
	d.fav.list = []UserRef{{UserID: "u2", Username: "bob"}}
	got, err := s.ListFavorites(context.Background(), auth.Identity{UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Username != "bob" {
		t.Fatalf("got %+v", got)
	}
}

// ── Invites ─────────────────────────────────────────────────────────────────

func TestCreateInvite(t *testing.T) {
	s, d := buildSvc()
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	s.newToken = func() string { return "tok123" }

	link, err := s.CreateInvite(context.Background(), auth.Identity{UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if link.Token != "tok123" || link.URL != "https://wa.test/i/tok123" {
		t.Fatalf("bad link: %+v", link)
	}
	if link.MaxUses != domain.DefaultInviteMaxUses {
		t.Errorf("MaxUses = %d, want %d", link.MaxUses, domain.DefaultInviteMaxUses)
	}
	if link.ExpiresAt != now.Add(domain.InviteTTL).UnixMilli() {
		t.Errorf("ExpiresAt = %d, want %d", link.ExpiresAt, now.Add(domain.InviteTTL).UnixMilli())
	}
	if _, ok := d.inv.byToken["tok123"]; !ok {
		t.Error("invite not persisted")
	}
}

func TestResolveInvite(t *testing.T) {
	s, d := buildSvc()
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	// unknown → 404
	_, err := s.ResolveInvite(context.Background(), "nope")
	assertAPIErr(t, err, 404, "INVITE_NOT_FOUND")

	// expired → 410
	d.inv.byToken["old"] = domain.Invite{Token: "old", InviterID: "u1", ExpiresAt: now.Add(-time.Hour), MaxUses: 5}
	_, err = s.ResolveInvite(context.Background(), "old")
	assertAPIErr(t, err, 410, "INVITE_EXPIRED")

	// valid → info
	d.inv.byToken["fresh"] = domain.Invite{Token: "fresh", InviterID: "u1", ExpiresAt: now.Add(time.Hour), MaxUses: 5, Uses: 2}
	info, err := s.ResolveInvite(context.Background(), "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if info.InviterID != "u1" || info.Uses != 2 || info.MaxUses != 5 {
		t.Fatalf("bad info: %+v", info)
	}
}

func TestRevokeInvite(t *testing.T) {
	s, d := buildSvc()
	d.inv.byToken["t"] = domain.Invite{Token: "t", InviterID: "u1", ExpiresAt: time.Now().Add(time.Hour)}

	// not owner → 404
	err := s.RevokeInvite(context.Background(), auth.Identity{UserID: "u2"}, "t")
	assertAPIErr(t, err, 404, "INVITE_NOT_FOUND")

	// owner → ok
	if err := s.RevokeInvite(context.Background(), auth.Identity{UserID: "u1"}, "t"); err != nil {
		t.Fatal(err)
	}
	if d.inv.byToken["t"].RevokedAt == nil {
		t.Error("revoked_at not set")
	}

	// unknown → 404
	err = s.RevokeInvite(context.Background(), auth.Identity{UserID: "u1"}, "ghost")
	assertAPIErr(t, err, 404, "INVITE_NOT_FOUND")
}
