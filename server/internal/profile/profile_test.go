package profile

import (
	"context"
	"errors"
	"testing"

	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

type fakeStore struct {
	profiles map[string]Profile
	setCalls []string // userID|ref, so a test can assert what was written
	getErr   error
}

func newFake() *fakeStore { return &fakeStore{profiles: map[string]Profile{}} }

func (s *fakeStore) Get(_ context.Context, userID string) (Profile, error) {
	if s.getErr != nil {
		return Profile{}, s.getErr
	}
	p, ok := s.profiles[userID]
	if !ok {
		return Profile{}, ErrNotFound
	}
	return p, nil
}

// Public mirrors the real store: it returns the row without applying privacy —
// that is the service's job, which is exactly what these tests pin down.
func (s *fakeStore) Public(_ context.Context, userID string) (Profile, error) {
	p, ok := s.profiles[userID]
	if !ok {
		return Profile{}, ErrNotFound
	}
	return Profile{UserID: p.UserID, Username: p.Username, DisplayName: p.DisplayName, About: p.About, AvatarRef: p.AvatarRef}, nil
}

func (s *fakeStore) Update(_ context.Context, userID, displayName, username, about string) error {
	p := s.profiles[userID]
	p.UserID, p.DisplayName, p.Username, p.About = userID, displayName, username, about
	s.profiles[userID] = p
	return nil
}

func (s *fakeStore) SetAvatar(_ context.Context, userID, ref string) error {
	s.setCalls = append(s.setCalls, userID+"|"+ref)
	p := s.profiles[userID]
	p.AvatarRef = ref
	s.profiles[userID] = p
	return nil
}

func (s *fakeStore) SetPrivacy(_ context.Context, userID string, privacy map[string]string) error {
	p := s.profiles[userID]
	p.Privacy = privacy
	s.profiles[userID] = p
	return nil
}
func (s *fakeStore) Block(context.Context, string, string) error   { return nil }
func (s *fakeStore) Unblock(context.Context, string, string) error { return nil }
func (s *fakeStore) Blocked(context.Context, string) ([]string, error) {
	return nil, nil
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func withAvatar(store *fakeStore, userID, ref string, privacy map[string]string) {
	store.profiles[userID] = Profile{UserID: userID, DisplayName: "Ada", AvatarRef: ref, Privacy: privacy}
}

// ── The avatar privacy setting ──────────────────────────────────────────────
//
// `avatar` has been in the privacy model since FR-USER-02 but nothing honoured
// it, because nothing served an avatar. Now that one is served it has to be
// enforced, and the default must be permissive so an unset preference does not
// hide everyone's picture.

func TestPublicServesAvatarWhenAllowed(t *testing.T) {
	st := newFake()
	svc := NewService(st)
	for _, privacy := range []map[string]string{
		nil,                     // unset → app default
		{"avatar": "everyone"},  // explicit
		{"last_seen": "nobody"}, // a different key restricted
	} {
		withAvatar(st, "ada", "media/ada.jpg", privacy)
		got, err := svc.Public(context.Background(), "ada")
		if err != nil {
			t.Fatalf("privacy %v: %v", privacy, err)
		}
		if got.AvatarRef != "media/ada.jpg" {
			t.Errorf("privacy %v: avatar should be served, got %q", privacy, got.AvatarRef)
		}
	}
}

func TestPublicWithholdsAvatarWhenRestricted(t *testing.T) {
	st := newFake()
	svc := NewService(st)
	// "contacts" is deliberately treated as "nobody" for now: this context has no
	// contact graph, and serving a picture the user restricted is the worse
	// failure of the two.
	for _, setting := range []string{"nobody", "contacts"} {
		withAvatar(st, "ada", "media/ada.jpg", map[string]string{"avatar": setting})
		got, err := svc.Public(context.Background(), "ada")
		if err != nil {
			t.Fatalf("%s: %v", setting, err)
		}
		if got.AvatarRef != "" {
			t.Errorf("avatar=%s must withhold the picture, got %q", setting, got.AvatarRef)
		}
		// The rest of the public profile is unaffected.
		if got.DisplayName != "Ada" {
			t.Errorf("%s: only the avatar should be withheld, name was %q", setting, got.DisplayName)
		}
	}
}

// If the owner's settings cannot be read we must not serve the picture anyway.
func TestPublicFailsClosedWhenPrivacyUnreadable(t *testing.T) {
	st := newFake()
	withAvatar(st, "ada", "media/ada.jpg", nil)
	st.getErr = errors.New("db down")
	got, err := NewService(st).Public(context.Background(), "ada")
	if err != nil {
		t.Fatal(err)
	}
	if got.AvatarRef != "" {
		t.Fatal("an unreadable privacy setting must withhold the avatar, not expose it")
	}
}

func TestPublicUnknownUser(t *testing.T) {
	if _, err := NewService(newFake()).Public(context.Background(), "ghost"); codeOf(t, err) != "USER_NOT_FOUND" {
		t.Fatal("unknown user should 404")
	}
}

// ── Setting the avatar ──────────────────────────────────────────────────────

func TestSetAvatarStoresAndClears(t *testing.T) {
	st := newFake()
	svc := NewService(st)
	ctx := context.Background()

	if err := svc.SetAvatar(ctx, "ada", "media/ada.jpg"); err != nil {
		t.Fatal(err)
	}
	if st.profiles["ada"].AvatarRef != "media/ada.jpg" {
		t.Fatalf("avatar not stored: %+v", st.profiles["ada"])
	}
	// An empty ref is the documented way to remove a picture.
	if err := svc.SetAvatar(ctx, "ada", ""); err != nil {
		t.Fatal(err)
	}
	if st.profiles["ada"].AvatarRef != "" {
		t.Fatal("an empty ref must clear the avatar")
	}
}

func TestSetAvatarRejectsOverlongRef(t *testing.T) {
	long := make([]byte, 301)
	for i := range long {
		long[i] = 'a'
	}
	err := NewService(newFake()).SetAvatar(context.Background(), "ada", string(long))
	if codeOf(t, err) != "VALIDATION_AVATAR" {
		t.Fatal("an object key is bounded; junk must be rejected")
	}
}

// The self view is not filtered: you always see your own picture, whatever your
// privacy setting says about showing it to others.
func TestGetAlwaysReturnsOwnAvatar(t *testing.T) {
	st := newFake()
	withAvatar(st, "ada", "media/ada.jpg", map[string]string{"avatar": "nobody"})
	got, err := NewService(st).Get(context.Background(), "ada")
	if err != nil {
		t.Fatal(err)
	}
	if got.AvatarRef != "media/ada.jpg" {
		t.Fatalf("own avatar must always be visible to self, got %q", got.AvatarRef)
	}
}
