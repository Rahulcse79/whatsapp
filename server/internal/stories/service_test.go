package stories

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/stories/domain"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeStore struct {
	byID    map[string]Story
	views   map[string][]Viewer
	purged  int
	purgeAt time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{byID: map[string]Story{}, views: map[string][]Viewer{}}
}
func (s *fakeStore) Create(_ context.Context, st Story) error { s.byID[st.ID] = st; return nil }
func (s *fakeStore) Get(_ context.Context, id string) (Story, error) {
	st, ok := s.byID[id]
	if !ok {
		return Story{}, ErrNotFound
	}
	return st, nil
}
func (s *fakeStore) Feed(_ context.Context, viewer string, now time.Time) ([]Story, error) {
	var out []Story
	for _, st := range s.byID {
		if now.After(st.ExpiresAt) {
			continue
		}
		for _, a := range st.Audience {
			if a == viewer {
				out = append(out, st)
				break
			}
		}
	}
	return out, nil
}
func (s *fakeStore) RecordView(_ context.Context, storyID, viewer string) error {
	for _, v := range s.views[storyID] {
		if v.UserID == viewer {
			return nil // idempotent
		}
	}
	s.views[storyID] = append(s.views[storyID], Viewer{UserID: viewer, ViewedAt: 0})
	return nil
}
func (s *fakeStore) Viewers(_ context.Context, storyID string) ([]Viewer, error) {
	return s.views[storyID], nil
}
func (s *fakeStore) Delete(_ context.Context, id string) error { delete(s.byID, id); return nil }
func (s *fakeStore) PurgeExpired(_ context.Context, now time.Time) (int, error) {
	s.purgeAt = now
	n := 0
	for id, st := range s.byID {
		if now.After(st.ExpiresAt) {
			delete(s.byID, id)
			n++
		}
	}
	s.purged = n
	return n, nil
}

type fakeAudience struct{ contacts map[string][]string }

func (a fakeAudience) ContactsOf(_ context.Context, author string) ([]string, error) {
	return a.contacts[author], nil
}

type harness struct {
	svc   *Service
	store *fakeStore
	now   time.Time
	seq   int
}

func newHarness() *harness {
	h := &harness{store: newFakeStore(), now: time.Unix(1_800_000_000, 0)}
	aud := fakeAudience{contacts: map[string][]string{"author": {"bob", "carol"}}}
	h.svc = NewService(h.store, aud)
	h.svc.now = func() time.Time { return h.now }
	h.svc.newID = func() string { h.seq++; return "story" + string(rune('0'+h.seq)) }
	return h
}

func who(u string) auth.Identity { return auth.Identity{UserID: u, DeviceID: "d1", SessionID: "s"} }

func code(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func ref(s string) *string { return &s }

// ── tests ────────────────────────────────────────────────────────────────

func TestPost_DefaultAudienceFromContacts(t *testing.T) {
	h := newHarness()
	res, err := h.svc.Post(context.Background(), who("author"), "image", ref("media-1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExpiresAt != h.now.Add(24*time.Hour).UnixMilli() {
		t.Fatalf("expiry = %d, want now+24h", res.ExpiresAt)
	}
	st := h.store.byID[res.StoryID]
	// audience = author + contacts, deduped.
	if len(st.Audience) != 3 {
		t.Fatalf("audience = %v, want [author bob carol]", st.Audience)
	}
}

func TestPost_OverrideAudienceAndValidation(t *testing.T) {
	h := newHarness()
	res, err := h.svc.Post(context.Background(), who("author"), "text", nil, []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	st := h.store.byID[res.StoryID]
	if len(st.Audience) != 2 { // author + bob
		t.Fatalf("override audience = %v, want [author bob]", st.Audience)
	}

	if _, err := h.svc.Post(context.Background(), who("author"), "image", nil, []string{"bob"}); code(t, err) != "VALIDATION_MEDIA" {
		t.Fatal("image without media ref should be VALIDATION_MEDIA")
	}
	if _, err := h.svc.Post(context.Background(), who("author"), "gif", ref("m"), []string{"bob"}); code(t, err) != "VALIDATION_KIND" {
		t.Fatal("bad kind should be VALIDATION_KIND")
	}
}

func TestFeed_FiltersByAudienceAndExpiry(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Post(context.Background(), who("author"), "image", ref("m1"), nil) // bob, carol in audience

	feed, err := h.svc.Feed(context.Background(), who("bob"))
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 1 || feed[0].StoryID != res.StoryID || !feed[0].KeyAvailable {
		t.Fatalf("bob feed = %+v, want the story w/ key_available", feed)
	}
	// A non-audience user sees nothing.
	if f, _ := h.svc.Feed(context.Background(), who("mallory")); len(f) != 0 {
		t.Fatalf("mallory feed = %+v, want empty", f)
	}
	// After expiry, the author's own feed is empty.
	h.now = h.now.Add(25 * time.Hour)
	if f, _ := h.svc.Feed(context.Background(), who("bob")); len(f) != 0 {
		t.Fatalf("expired feed = %+v, want empty", f)
	}
}

func TestView_AudienceOnly(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Post(context.Background(), who("author"), "image", ref("m1"), nil)

	if err := h.svc.View(context.Background(), who("bob"), res.StoryID); err != nil {
		t.Fatal(err)
	}
	if len(h.store.views[res.StoryID]) != 1 {
		t.Fatal("view should be recorded")
	}
	// Idempotent re-view.
	_ = h.svc.View(context.Background(), who("bob"), res.StoryID)
	if len(h.store.views[res.StoryID]) != 1 {
		t.Fatal("re-view must not duplicate")
	}
	// Non-audience → 404 (never confirm existence).
	if err := h.svc.View(context.Background(), who("mallory"), res.StoryID); code(t, err) != "STORY_NOT_FOUND" {
		t.Fatal("non-audience view should be STORY_NOT_FOUND")
	}
	// Unknown → 404.
	if err := h.svc.View(context.Background(), who("bob"), "nope"); code(t, err) != "STORY_NOT_FOUND" {
		t.Fatal("unknown story should be STORY_NOT_FOUND")
	}
}

func TestViewersAndDelete_AuthorOnly(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Post(context.Background(), who("author"), "image", ref("m1"), nil)
	_ = h.svc.View(context.Background(), who("bob"), res.StoryID)

	// Author sees viewers.
	vs, err := h.svc.Viewers(context.Background(), who("author"), res.StoryID)
	if err != nil || len(vs) != 1 || vs[0].UserID != "bob" {
		t.Fatalf("viewers = %+v (%v), want [bob]", vs, err)
	}
	// Non-author → 404.
	if _, err := h.svc.Viewers(context.Background(), who("bob"), res.StoryID); code(t, err) != "STORY_NOT_FOUND" {
		t.Fatal("non-author viewers should be STORY_NOT_FOUND")
	}
	// Non-author delete → 404, story survives.
	if err := h.svc.Delete(context.Background(), who("bob"), res.StoryID); code(t, err) != "STORY_NOT_FOUND" {
		t.Fatal("non-author delete should be STORY_NOT_FOUND")
	}
	if _, ok := h.store.byID[res.StoryID]; !ok {
		t.Fatal("story must survive a non-author delete")
	}
	// Author deletes.
	if err := h.svc.Delete(context.Background(), who("author"), res.StoryID); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.store.byID[res.StoryID]; ok {
		t.Fatal("author delete should remove the story")
	}
}

func TestPurgeExpired(t *testing.T) {
	h := newHarness()
	_, _ = h.svc.Post(context.Background(), who("author"), "image", ref("m1"), nil)
	h.now = h.now.Add(25 * time.Hour)
	n, err := h.svc.PurgeExpired(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("purge = (%d,%v), want (1,nil)", n, err)
	}
	if !h.store.purgeAt.Equal(h.now) {
		t.Fatal("purge should use the service clock")
	}
	_ = domain.TTL // keep domain imported
}
