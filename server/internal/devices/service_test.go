package devices

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeRepo struct {
	mu      sync.Mutex
	devs    map[string]Device // by device id
	renamed map[string]string
	revoked map[string]bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{devs: map[string]Device{}, renamed: map[string]string{}, revoked: map[string]bool{}}
}
func (f *fakeRepo) add(d Device) { f.mu.Lock(); f.devs[d.ID] = d; f.mu.Unlock() }
func (f *fakeRepo) ListByUser(_ context.Context, userID string) ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Device
	for _, d := range f.devs {
		if d.UserID == userID && !d.Revoked {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f *fakeRepo) Get(_ context.Context, deviceID string) (Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.devs[deviceID]
	if !ok {
		return Device{}, ErrNotFound
	}
	return d, nil
}
func (f *fakeRepo) Rename(_ context.Context, userID, deviceID, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.devs[deviceID]
	if !ok || d.UserID != userID || d.Revoked {
		return false, nil
	}
	f.renamed[deviceID] = name
	return true, nil
}
func (f *fakeRepo) CountActive(_ context.Context, userID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.devs {
		if d.UserID == userID && !d.Revoked {
			n++
		}
	}
	return n, nil
}
func (f *fakeRepo) RevokeDevice(_ context.Context, userID, deviceID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.devs[deviceID]
	if !ok || d.UserID != userID || d.Revoked {
		return false, nil
	}
	d.Revoked = true
	f.devs[deviceID] = d
	f.revoked[deviceID] = true
	return true, nil
}

type fakeLinks struct {
	mu sync.Mutex
	m  map[string]Link
}

func newFakeLinks() *fakeLinks { return &fakeLinks{m: map[string]Link{}} }
func (f *fakeLinks) CreateLink(_ context.Context, l Link) error {
	f.mu.Lock()
	f.m[l.Token] = l
	f.mu.Unlock()
	return nil
}
func (f *fakeLinks) GetLink(_ context.Context, token string) (Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.m[token]
	if !ok {
		return Link{}, ErrNotFound
	}
	return l, nil
}
func (f *fakeLinks) ApproveLink(_ context.Context, p ApproveParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.m[p.Token]
	if !ok || l.State != LinkPending {
		return ErrNotFound
	}
	l.State = LinkApproved
	l.UserID = p.UserID
	l.DeviceID = p.NewDeviceID
	l.ApprovedBy = p.ApprovedBy
	l.Cert = p.Cert
	f.m[p.Token] = l
	return nil
}
func (f *fakeLinks) ConsumeLink(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.m[token]
	l.State = LinkConsumed
	f.m[token] = l
	return nil
}

type fakeMinter struct{ calls int }

func (m *fakeMinter) MintLinkedSession(_ context.Context, userID, deviceID string) (string, string, string, error) {
	m.calls++
	return "access-" + deviceID, "refresh-" + deviceID, "sess-" + deviceID, nil
}

type recEvents struct {
	added, revoked []string
}

func (e *recEvents) DeviceAdded(_ context.Context, _, deviceID string) {
	e.added = append(e.added, deviceID)
}
func (e *recEvents) DeviceRevoked(_ context.Context, _, deviceID string) {
	e.revoked = append(e.revoked, deviceID)
}

type harness struct {
	svc    *Service
	repo   *fakeRepo
	links  *fakeLinks
	minter *fakeMinter
	events *recEvents
}

func newHarness() *harness {
	repo, links, minter, events := newFakeRepo(), newFakeLinks(), &fakeMinter{}, &recEvents{}
	return &harness{svc: NewService(repo, links, minter, events), repo: repo, links: links, minter: minter, events: events}
}

func apiCode(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

var primaryIdent = auth.Identity{UserID: "u1", DeviceID: "primary1", SessionID: "s1"}

func withPrimary(h *harness) {
	h.repo.add(Device{ID: "primary1", UserID: "u1", IsPrimary: true, Platform: PlatformAndroid, Name: "Pixel"})
}

// ── tests ─────────────────────────────────────────────────────────────────

func TestList(t *testing.T) {
	h := newHarness()
	withPrimary(h)
	h.repo.add(Device{ID: "d2", UserID: "u1", Platform: PlatformWeb, Name: "Web"})
	h.repo.add(Device{ID: "other", UserID: "u2"})

	views, err := h.svc.List(context.Background(), primaryIdent)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("want 2 own devices, got %d", len(views))
	}
	for _, v := range views {
		if v.ID == "primary1" && (!v.IsPrimary || v.Platform != "android") {
			t.Fatalf("primary view wrong: %+v", v)
		}
	}
}

func TestRename(t *testing.T) {
	h := newHarness()
	withPrimary(h)

	if err := h.svc.Rename(context.Background(), primaryIdent, "primary1", "My Phone"); err != nil {
		t.Fatal(err)
	}
	if h.repo.renamed["primary1"] != "My Phone" {
		t.Fatal("rename not applied")
	}
	if err := h.svc.Rename(context.Background(), primaryIdent, "primary1", ""); apiCode(t, err) != "VALIDATION_NAME" {
		t.Fatal("empty name accepted")
	}
	if err := h.svc.Rename(context.Background(), primaryIdent, "ghost", "X"); apiCode(t, err) != "DEVICE_NOT_FOUND" {
		t.Fatal("rename of foreign device not rejected")
	}
}

func TestRevoke_EmitsEvent(t *testing.T) {
	h := newHarness()
	withPrimary(h)
	h.repo.add(Device{ID: "d2", UserID: "u1"})

	if err := h.svc.Revoke(context.Background(), primaryIdent, "d2"); err != nil {
		t.Fatal(err)
	}
	if !h.repo.revoked["d2"] {
		t.Fatal("device not revoked")
	}
	if len(h.events.revoked) != 1 || h.events.revoked[0] != "d2" {
		t.Fatalf("device_revoked event not emitted: %v", h.events.revoked)
	}
	// Revoking again → not found (already revoked).
	if err := h.svc.Revoke(context.Background(), primaryIdent, "d2"); apiCode(t, err) != "DEVICE_NOT_FOUND" {
		t.Fatal("double revoke not rejected")
	}
}

func TestLinkFlow_Full(t *testing.T) {
	h := newHarness()
	withPrimary(h)
	ctx := context.Background()

	// init
	init, err := h.svc.LinkInit(ctx, LinkInitRequest{Platform: "web", Name: "Laptop", IdentityKey: []byte("newpub")})
	if err != nil {
		t.Fatal(err)
	}
	if init.LinkToken == "" || init.QRPayload == "" {
		t.Fatal("empty init result")
	}

	// polling before approval → pending
	poll, err := h.svc.LinkComplete(ctx, init.LinkToken)
	if err != nil || !poll.Pending {
		t.Fatalf("want pending, got %+v err=%v", poll, err)
	}

	// approve (primary)
	if err := h.svc.LinkApprove(ctx, primaryIdent, init.LinkToken, []byte("signature")); err != nil {
		t.Fatal(err)
	}
	if len(h.events.added) != 1 {
		t.Fatal("device_added event not emitted")
	}

	// complete → tokens
	done, err := h.svc.LinkComplete(ctx, init.LinkToken)
	if err != nil {
		t.Fatal(err)
	}
	if done.AccessJWT == "" || done.RefreshToken == "" || done.DeviceID == "" || done.Pending {
		t.Fatalf("completion did not return tokens: %+v", done)
	}
	if h.minter.calls != 1 {
		t.Fatalf("minter called %d times, want 1", h.minter.calls)
	}

	// second completion → already used
	if _, err := h.svc.LinkComplete(ctx, init.LinkToken); apiCode(t, err) != "LINK_ALREADY_USED" {
		t.Fatal("double completion not rejected")
	}
}

func TestLinkApprove_NotPrimary(t *testing.T) {
	h := newHarness()
	h.repo.add(Device{ID: "linked1", UserID: "u1", IsPrimary: false})
	ctx := context.Background()
	init, _ := h.svc.LinkInit(ctx, LinkInitRequest{Platform: "web", IdentityKey: []byte("k")})

	nonPrimary := auth.Identity{UserID: "u1", DeviceID: "linked1"}
	if err := h.svc.LinkApprove(ctx, nonPrimary, init.LinkToken, []byte("sig")); apiCode(t, err) != "NOT_PRIMARY_DEVICE" {
		t.Fatal("non-primary approval not rejected")
	}
}

func TestLinkApprove_RequiresCert(t *testing.T) {
	h := newHarness()
	withPrimary(h)
	ctx := context.Background()
	init, _ := h.svc.LinkInit(ctx, LinkInitRequest{Platform: "web", IdentityKey: []byte("k")})
	if err := h.svc.LinkApprove(ctx, primaryIdent, init.LinkToken, nil); apiCode(t, err) != "VALIDATION_CERT" {
		t.Fatal("approval without cert not rejected")
	}
}

func TestLinkApprove_DeviceLimit(t *testing.T) {
	h := newHarness()
	withPrimary(h)
	for i := 0; i < MaxDevicesPerUser-1; i++ {
		h.repo.add(Device{ID: string(rune('a' + i)), UserID: "u1"})
	}
	ctx := context.Background()
	init, _ := h.svc.LinkInit(ctx, LinkInitRequest{Platform: "web", IdentityKey: []byte("k")})
	if err := h.svc.LinkApprove(ctx, primaryIdent, init.LinkToken, []byte("sig")); apiCode(t, err) != "STATE_DEVICE_LIMIT" {
		t.Fatal("device limit not enforced at approval")
	}
}

func TestLinkInit_Validation(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.LinkInit(context.Background(), LinkInitRequest{Platform: "toaster", IdentityKey: []byte("k")}); apiCode(t, err) != "VALIDATION_DEVICE" {
		t.Fatal("bad platform accepted")
	}
	if _, err := h.svc.LinkInit(context.Background(), LinkInitRequest{Platform: "web"}); apiCode(t, err) != "VALIDATION_DEVICE" {
		t.Fatal("missing identity key accepted")
	}
}

func TestLinkComplete_Expired(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	init, _ := h.svc.LinkInit(ctx, LinkInitRequest{Platform: "web", IdentityKey: []byte("k")})
	// Force the clock past expiry.
	h.svc.now = func() time.Time { return time.Now().Add(LinkTTL + time.Minute) }
	if _, err := h.svc.LinkComplete(ctx, init.LinkToken); apiCode(t, err) != "LINK_EXPIRED" {
		t.Fatal("expired link not rejected")
	}
}
