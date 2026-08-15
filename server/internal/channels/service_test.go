package channels

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/channels/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── in-memory fake Store ────────────────────────────────────────────────────

type fakeStore struct {
	channels  map[string]Channel
	members   map[string]Member // key channelID|userID
	posts     map[string]Post
	reactions map[string]map[string]int // postID → emoji → count (dedup ignored in fake)
	comments  map[string]Comment
	handles   map[string]bool
	views     map[string]int64            // postID → views
	subs      map[string]map[string]int64 // channelID → userID → expiry ms
}

func newFake() *fakeStore {
	return &fakeStore{
		channels: map[string]Channel{}, members: map[string]Member{}, posts: map[string]Post{},
		reactions: map[string]map[string]int{}, comments: map[string]Comment{}, handles: map[string]bool{},
		views: map[string]int64{}, subs: map[string]map[string]int64{},
	}
}

func mk(a, b string) string { return a + "|" + b }

func (f *fakeStore) CreateChannel(_ context.Context, c Channel) error {
	if f.handles[c.Handle] {
		return ErrHandleTaken
	}
	f.handles[c.Handle] = true
	f.channels[c.ID] = c
	return nil
}
func (f *fakeStore) GetChannel(_ context.Context, id string) (Channel, error) {
	c, ok := f.channels[id]
	if !ok {
		return Channel{}, ErrNotFound
	}
	return c, nil
}
func (f *fakeStore) UpdateChannel(_ context.Context, id string, name, description *string) error {
	c := f.channels[id]
	if name != nil {
		c.Name = *name
	}
	if description != nil {
		c.Description = *description
	}
	f.channels[id] = c
	return nil
}
func (f *fakeStore) DeleteChannel(_ context.Context, id string) error {
	delete(f.channels, id)
	return nil
}
func (f *fakeStore) SearchChannels(_ context.Context, _ string, _ int) ([]Channel, error) {
	return nil, nil
}
func (f *fakeStore) Discover(_ context.Context, _ int) ([]Channel, error) { return nil, nil }
func (f *fakeStore) FollowerCount(_ context.Context, channelID string) (int, error) {
	n := 0
	for _, m := range f.members {
		if m.ChannelID == channelID {
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) GetMember(_ context.Context, channelID, userID string) (Member, error) {
	m, ok := f.members[mk(channelID, userID)]
	if !ok {
		return Member{}, ErrNotFound
	}
	return m, nil
}
func (f *fakeStore) AddMember(_ context.Context, m Member) error {
	if _, ok := f.members[mk(m.ChannelID, m.UserID)]; ok {
		return nil // ON CONFLICT DO NOTHING
	}
	f.members[mk(m.ChannelID, m.UserID)] = m
	return nil
}
func (f *fakeStore) RemoveMember(_ context.Context, channelID, userID string) error {
	delete(f.members, mk(channelID, userID))
	return nil
}
func (f *fakeStore) SetRole(_ context.Context, channelID, userID string, role domain.Role) error {
	m, ok := f.members[mk(channelID, userID)]
	if !ok {
		return ErrNotFound
	}
	m.Role = role
	f.members[mk(channelID, userID)] = m
	return nil
}
func (f *fakeStore) ListMembers(_ context.Context, channelID string, _ int) ([]Member, error) {
	var out []Member
	for _, m := range f.members {
		if m.ChannelID == channelID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (f *fakeStore) CreatePost(_ context.Context, p Post) error { f.posts[p.ID] = p; return nil }
func (f *fakeStore) GetPost(_ context.Context, postID string) (Post, error) {
	p, ok := f.posts[postID]
	if !ok {
		return Post{}, ErrNotFound
	}
	return p, nil
}
func (f *fakeStore) ListPosts(_ context.Context, channelID string, _ int) ([]Post, error) {
	var out []Post
	for _, p := range f.posts {
		if p.ChannelID == channelID && p.Published {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakeStore) DeletePost(_ context.Context, postID string) error {
	delete(f.posts, postID)
	return nil
}
func (f *fakeStore) PublishDue(_ context.Context, now time.Time, _ int) ([]Post, error) {
	var out []Post
	for id, p := range f.posts {
		if !p.Published && !p.PublishAt.After(now) {
			p.Published = true
			f.posts[id] = p
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakeStore) React(_ context.Context, postID, _, emoji string) error {
	if f.reactions[postID] == nil {
		f.reactions[postID] = map[string]int{}
	}
	f.reactions[postID][emoji]++
	return nil
}
func (f *fakeStore) Unreact(_ context.Context, postID, _, emoji string) error {
	if f.reactions[postID] != nil {
		f.reactions[postID][emoji]--
	}
	return nil
}
func (f *fakeStore) Reactions(_ context.Context, postID string) (map[string]int, error) {
	return f.reactions[postID], nil
}
func (f *fakeStore) CreateComment(_ context.Context, c Comment) error {
	f.comments[c.ID] = c
	return nil
}
func (f *fakeStore) ListComments(_ context.Context, postID string, _ int) ([]Comment, error) {
	var out []Comment
	for _, c := range f.comments {
		if c.PostID == postID {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *fakeStore) CommentCount(_ context.Context, postID string) (int, error) {
	n := 0
	for _, c := range f.comments {
		if c.PostID == postID {
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) GetComment(_ context.Context, id string) (Comment, error) {
	c, ok := f.comments[id]
	if !ok {
		return Comment{}, ErrNotFound
	}
	return c, nil
}
func (f *fakeStore) DeleteComment(_ context.Context, id string) error {
	delete(f.comments, id)
	return nil
}
func (f *fakeStore) IncrementViews(_ context.Context, postID string) error {
	f.views[postID]++
	return nil
}
func (f *fakeStore) Insights(_ context.Context, channelID string) (Insights, error) {
	c := f.channels[channelID]
	ins := Insights{ChannelID: channelID, Premium: c.Premium, PriceCents: c.PriceCents}
	for _, m := range f.members {
		if m.ChannelID == channelID {
			ins.Followers++
		}
	}
	for range f.subs[channelID] {
		ins.Subscribers++
	}
	for _, p := range f.posts {
		if p.ChannelID == channelID && p.Published {
			ins.Posts++
			ins.Views += f.views[p.ID]
		}
	}
	for pid, rx := range f.reactions {
		if f.posts[pid].ChannelID == channelID {
			for _, n := range rx {
				ins.Reactions += n
			}
		}
	}
	for _, cm := range f.comments {
		if f.posts[cm.PostID].ChannelID == channelID {
			ins.Comments++
		}
	}
	return ins, nil
}
func (f *fakeStore) SetPremium(_ context.Context, channelID string, premium bool, priceCents int) error {
	c := f.channels[channelID]
	c.Premium = premium
	c.PriceCents = priceCents
	f.channels[channelID] = c
	return nil
}
func (f *fakeStore) Subscribe(_ context.Context, channelID, userID, _ string, expiresAt time.Time) error {
	if f.subs[channelID] == nil {
		f.subs[channelID] = map[string]int64{}
	}
	f.subs[channelID][userID] = expiresAt.UnixMilli()
	return nil
}
func (f *fakeStore) IsSubscribed(_ context.Context, channelID, userID string, now time.Time) (bool, error) {
	exp, ok := f.subs[channelID][userID]
	return ok && exp > now.UnixMilli(), nil
}

type fakeBroadcaster struct{ posts int }

func (b *fakeBroadcaster) PostPublished(_ context.Context, _, _ string) error { b.posts++; return nil }

type okGateway struct{}

func (okGateway) Charge(_ context.Context, _, _ string, _ int) (string, error) {
	return "test-ref", nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func newSvc() (*Service, *fakeStore, *fakeBroadcaster) {
	f := newFake()
	b := &fakeBroadcaster{}
	s := NewService(f, b, okGateway{}, nil)
	n := 0
	s.newID = func() string { n++; return "id" + string(rune('A'+n)) }
	s.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return s, f, b
}

func ident(u string) auth.Identity { return auth.Identity{UserID: u} }

func status(t *testing.T, err error) int {
	t.Helper()
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	t.Fatalf("expected an APIError, got %v", err)
	return 0
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestCreateAndOwnership(t *testing.T) {
	s, f, _ := newSvc()
	ctx := context.Background()
	c, err := s.Create(ctx, ident("owner"), "News", "Daily News", "desc", "public")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.MyRole != "owner" || c.Handle != "news" || c.Followers != 1 {
		t.Fatalf("owner view wrong: %+v", c)
	}
	if m := f.members[mk(c.ID, "owner")]; m.Role != domain.RoleOwner {
		t.Fatal("owner should be a role-owner member")
	}
	// Duplicate handle → 409.
	if _, err := s.Create(ctx, ident("other"), "news", "Copy", "", "public"); status(t, err) != 409 {
		t.Fatal("duplicate handle must 409")
	}
	// Bad handle → 400.
	if _, err := s.Create(ctx, ident("x"), "no", "N", "", "public"); status(t, err) != 400 {
		t.Fatal("short handle must 400")
	}
}

func TestFollowAndPrivacy(t *testing.T) {
	s, _, _ := newSvc()
	ctx := context.Background()
	pub, _ := s.Create(ctx, ident("owner"), "pub", "Public", "", "public")
	priv, _ := s.Create(ctx, ident("owner"), "priv", "Private", "", "private")

	// Follow public → follower.
	if err := s.Follow(ctx, ident("alice"), pub.ID); err != nil {
		t.Fatalf("follow public: %v", err)
	}
	got, _ := s.Get(ctx, ident("alice"), pub.ID)
	if got.MyRole != "follower" || got.Followers != 2 {
		t.Fatalf("after follow: %+v", got)
	}
	// Non-member Get of a PRIVATE channel → 404 (can't probe).
	if _, err := s.Get(ctx, ident("bob"), priv.ID); status(t, err) != 404 {
		t.Fatal("private channel must 404 for non-members")
	}
	// Self-serve follow of a private channel → 404.
	if err := s.Follow(ctx, ident("bob"), priv.ID); status(t, err) != 404 {
		t.Fatal("private follow must 404")
	}
	// Owner cannot unfollow their own channel.
	if err := s.Unfollow(ctx, ident("owner"), pub.ID); status(t, err) != 409 {
		t.Fatal("owner unfollow must 409")
	}
}

func TestPostingPermissionsAndBroadcast(t *testing.T) {
	s, _, b := newSvc()
	ctx := context.Background()
	ch, _ := s.Create(ctx, ident("owner"), "news", "News", "", "public")
	_ = s.Follow(ctx, ident("alice"), ch.ID)

	// A follower cannot post → 404 (permission hidden).
	if _, err := s.CreatePost(ctx, ident("alice"), ch.ID, "hi", nil, 0); status(t, err) != 404 {
		t.Fatal("follower post must be denied")
	}
	// The owner posts immediately → broadcast fires.
	p, err := s.CreatePost(ctx, ident("owner"), ch.ID, "Breaking news", nil, 0)
	if err != nil {
		t.Fatalf("owner post: %v", err)
	}
	if p.Scheduled || b.posts != 1 {
		t.Fatalf("immediate post should broadcast once, got scheduled=%v broadcasts=%d", p.Scheduled, b.posts)
	}
	// A scheduled (future) post does NOT broadcast yet.
	future := s.now().Add(time.Hour).UnixMilli()
	sp, _ := s.CreatePost(ctx, ident("owner"), ch.ID, "Later", nil, future)
	if !sp.Scheduled || b.posts != 1 {
		t.Fatalf("scheduled post must not broadcast: scheduled=%v broadcasts=%d", sp.Scheduled, b.posts)
	}
	// The feed shows only the published post (public → alice can read).
	feed, _ := s.Posts(ctx, ident("alice"), ch.ID, 0)
	if len(feed) != 1 || feed[0].Body != "Breaking news" {
		t.Fatalf("feed should show only the published post, got %+v", feed)
	}
	// Advancing time + PublishDue publishes the scheduled one and broadcasts it.
	s.now = func() time.Time { return time.UnixMilli(future + 1) }
	n, _ := s.PublishDue(ctx)
	if n != 1 || b.posts != 2 {
		t.Fatalf("PublishDue should publish+broadcast 1, got n=%d broadcasts=%d", n, b.posts)
	}
}

func TestReactionsCommentsAndRoles(t *testing.T) {
	s, _, _ := newSvc()
	ctx := context.Background()
	ch, _ := s.Create(ctx, ident("owner"), "news", "News", "", "public")
	p, _ := s.CreatePost(ctx, ident("owner"), ch.ID, "Hello followers", nil, 0)
	_ = s.Follow(ctx, ident("alice"), ch.ID)

	// A viewer reacts + comments.
	if err := s.React(ctx, ident("alice"), p.ID, "👍", true); err != nil {
		t.Fatalf("react: %v", err)
	}
	cm, err := s.Comment(ctx, ident("alice"), p.ID, "great post")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	feed, _ := s.Posts(ctx, ident("owner"), ch.ID, 0)
	if feed[0].Reactions["👍"] != 1 || feed[0].Comments != 1 {
		t.Fatalf("post aggregates wrong: %+v", feed[0])
	}
	// Only the owner changes roles; promoting alice to admin lets her post.
	if err := s.SetRole(ctx, ident("alice"), ch.ID, "owner", "admin"); status(t, err) != 404 {
		t.Fatal("non-owner cannot change roles")
	}
	if err := s.SetRole(ctx, ident("owner"), ch.ID, "alice", "admin"); err != nil {
		t.Fatalf("owner promote: %v", err)
	}
	if _, err := s.CreatePost(ctx, ident("alice"), ch.ID, "now I can post", nil, 0); err != nil {
		t.Fatalf("promoted admin should post: %v", err)
	}
	// A commenter can delete their own comment.
	if err := s.DeleteComment(ctx, ident("alice"), cm.ID); err != nil {
		t.Fatalf("author delete comment: %v", err)
	}
	// A stranger cannot delete someone else's comment.
	cm2, _ := s.Comment(ctx, ident("alice"), p.ID, "second")
	if err := s.DeleteComment(ctx, ident("stranger"), cm2.ID); status(t, err) != 404 {
		t.Fatal("stranger cannot delete a comment")
	}
}

func TestAnalyticsAndPremium(t *testing.T) {
	s, _, _ := newSvc()
	ctx := context.Background()
	ch, _ := s.Create(ctx, ident("owner"), "news", "News", "", "public")
	p, _ := s.CreatePost(ctx, ident("owner"), ch.ID, "Hello", nil, 0)
	_ = s.Follow(ctx, ident("alice"), ch.ID)

	// Views accumulate; insights aggregate them (channel-admin only).
	for i := 0; i < 3; i++ {
		if err := s.RecordView(ctx, ident("alice"), p.ID); err != nil {
			t.Fatalf("view: %v", err)
		}
	}
	if _, err := s.GetInsights(ctx, ident("alice"), ch.ID); status(t, err) != 404 {
		t.Fatal("non-admin cannot read insights")
	}
	ins, err := s.GetInsights(ctx, ident("owner"), ch.ID)
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	if ins.Views != 3 || ins.Followers != 2 || ins.Posts != 1 {
		t.Fatalf("insights wrong: %+v", ins)
	}

	// Premium: only the owner may set it; then non-subscribers lose feed access.
	if err := s.SetPremium(ctx, ident("alice"), ch.ID, true, 500); status(t, err) != 404 {
		t.Fatal("non-owner cannot set premium")
	}
	if err := s.SetPremium(ctx, ident("owner"), ch.ID, true, 500); err != nil {
		t.Fatalf("set premium: %v", err)
	}
	if _, err := s.Posts(ctx, ident("alice"), ch.ID, 0); status(t, err) != 402 {
		t.Fatal("non-subscriber must get 402 on a premium feed")
	}
	// The owner still reads their own premium channel.
	if _, err := s.Posts(ctx, ident("owner"), ch.ID, 0); err != nil {
		t.Fatalf("owner reads premium feed: %v", err)
	}

	// Subscribe (payment seam) grants access + reflects in the view.
	res, err := s.Subscribe(ctx, ident("alice"), ch.ID)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if res.PaymentRef != "test-ref" || res.ExpiresAtMS <= s.now().UnixMilli() {
		t.Fatalf("subscribe result wrong: %+v", res)
	}
	if _, err := s.Posts(ctx, ident("alice"), ch.ID, 0); err != nil {
		t.Fatalf("subscriber reads premium feed: %v", err)
	}
	got, _ := s.Get(ctx, ident("alice"), ch.ID)
	if !got.Premium || !got.MySubscribed || got.PriceCents != 500 {
		t.Fatalf("subscriber channel view wrong: %+v", got)
	}
	// Subscribing to a free channel is rejected.
	free, _ := s.Create(ctx, ident("owner"), "free", "Free", "", "public")
	if _, err := s.Subscribe(ctx, ident("alice"), free.ID); status(t, err) != 409 {
		t.Fatal("cannot subscribe to a non-premium channel")
	}
}
