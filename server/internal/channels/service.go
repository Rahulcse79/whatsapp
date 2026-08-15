package channels

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/channels/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// subscriptionPeriod is how long one charge grants premium access.
const subscriptionPeriod = 30 * 24 * time.Hour

// Service orchestrates the channel lifecycle, membership, posting, reactions,
// comments, analytics, and premium subscriptions, enforcing the domain
// permission matrix.
type Service struct {
	store       Store
	broadcaster Broadcaster
	payments    PaymentGateway
	log         *slog.Logger
	now         func() time.Time
	newID       func() string
}

func NewService(store Store, broadcaster Broadcaster, payments PaymentGateway, log *slog.Logger) *Service {
	return &Service{store: store, broadcaster: broadcaster, payments: payments, log: log, now: time.Now, newID: id.New}
}

// ── channels ────────────────────────────────────────────────────────────────

// Create registers a channel; the creator becomes its owner. Handle is
// case-folded and must be unique among live channels.
func (s *Service) Create(ctx context.Context, ident auth.Identity, handle, name, description string, kindStr string) (ChannelView, error) {
	handle = strings.ToLower(strings.TrimSpace(handle))
	kind := domain.KindPublic
	if kindStr == "private" {
		kind = domain.KindPrivate
	} else if kindStr != "" && kindStr != "public" {
		return ChannelView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_KIND", "kind must be public or private")
	}
	if err := domain.ValidateCreate(handle, name, description, kind); err != nil {
		return ChannelView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_CHANNEL", err.Error())
	}
	c := Channel{
		ID: s.newID(), OwnerID: ident.UserID, Handle: handle, Name: name,
		Description: description, Kind: kind, CreatedAt: s.now(),
	}
	if err := s.store.CreateChannel(ctx, c); err != nil {
		if errors.Is(err, ErrHandleTaken) {
			return ChannelView{}, httpx.Reject(http.StatusConflict, "HANDLE_TAKEN", "that channel handle is already taken")
		}
		return ChannelView{}, httpx.Transient()
	}
	// The owner is member role=owner.
	if err := s.store.AddMember(ctx, Member{ChannelID: c.ID, UserID: ident.UserID, Role: domain.RoleOwner, JoinedAt: s.now()}); err != nil {
		return ChannelView{}, httpx.Transient()
	}
	return s.view(ctx, c, domain.RoleOwner, true, ident.UserID), nil
}

// Get returns a channel view. A private channel is 404 to non-members (can't
// probe private channels).
func (s *Service) Get(ctx context.Context, ident auth.Identity, channelID string) (ChannelView, error) {
	c, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return ChannelView{}, err
	}
	if c.Kind == domain.KindPrivate && !member {
		return ChannelView{}, s.notFound()
	}
	return s.view(ctx, c, me.Role, member, ident.UserID), nil
}

// Update edits name/description (admin+).
func (s *Service) Update(ctx context.Context, ident auth.Identity, channelID string, name, description *string) error {
	_, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return err
	}
	if !member || !domain.CanEditInfo(me.Role) {
		return s.notFound()
	}
	if name != nil && (*name == "" || len(*name) > domain.MaxName) {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_NAME", "name is required (max 80)")
	}
	if description != nil && len(*description) > domain.MaxDescription {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_DESC", "description too long (max 500)")
	}
	if err := s.store.UpdateChannel(ctx, channelID, name, description); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Delete removes a channel (owner only).
func (s *Service) Delete(ctx context.Context, ident auth.Identity, channelID string) error {
	_, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return err
	}
	if !member || !domain.CanDelete(me.Role) {
		return s.notFound()
	}
	if err := s.store.DeleteChannel(ctx, channelID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Search finds public channels by name/handle.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]ChannelView, error) {
	q := strings.TrimSpace(query)
	if len(q) < 2 {
		return nil, httpx.Reject(http.StatusBadRequest, "VALIDATION_QUERY", "query must be at least 2 characters")
	}
	cs, err := s.store.SearchChannels(ctx, q, clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	return s.publicViews(ctx, cs), nil
}

// Discover lists popular public channels.
func (s *Service) Discover(ctx context.Context, limit int) ([]ChannelView, error) {
	cs, err := s.store.Discover(ctx, clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	return s.publicViews(ctx, cs), nil
}

// ── membership ──────────────────────────────────────────────────────────────

// Follow subscribes the caller to a public channel (idempotent). Private
// channels are invite-only (an admin adds members via SetRole), so a self-serve
// follow of a private channel is 404 (it isn't discoverable to begin with).
func (s *Service) Follow(ctx context.Context, ident auth.Identity, channelID string) error {
	c, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return err
	}
	if c.Kind == domain.KindPrivate && !member {
		return s.notFound()
	}
	if member && me.Role >= domain.RoleAdmin {
		return nil // owner/admin already belong; don't demote them
	}
	if err := s.store.AddMember(ctx, Member{ChannelID: channelID, UserID: ident.UserID, Role: domain.RoleFollower, JoinedAt: s.now()}); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Unfollow removes the caller (the owner cannot unfollow their own channel).
func (s *Service) Unfollow(ctx context.Context, ident auth.Identity, channelID string) error {
	_, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return err
	}
	if member && me.Role == domain.RoleOwner {
		return httpx.Reject(http.StatusConflict, "OWNER_CANNOT_LEAVE", "delete the channel instead of leaving")
	}
	if err := s.store.RemoveMember(ctx, channelID, ident.UserID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Members lists followers/admins (admin+ — a channel's audience is not public).
func (s *Service) Members(ctx context.Context, ident auth.Identity, channelID string, limit int) ([]MemberView, error) {
	_, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return nil, err
	}
	if !member || !domain.CanEditInfo(me.Role) {
		return nil, s.notFound()
	}
	ms, err := s.store.ListMembers(ctx, channelID, clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]MemberView, 0, len(ms))
	for _, m := range ms {
		out = append(out, MemberView{UserID: m.UserID, Role: m.Role.String()})
	}
	return out, nil
}

// SetRole promotes/demotes a member between follower and admin (owner only).
func (s *Service) SetRole(ctx context.Context, ident auth.Identity, channelID, targetUserID string, roleStr string) error {
	_, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return err
	}
	if !member || !domain.CanChangeRoles(me.Role) {
		return s.notFound()
	}
	var role domain.Role
	switch roleStr {
	case "admin":
		role = domain.RoleAdmin
	case "follower":
		role = domain.RoleFollower
	default:
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_ROLE", "role must be admin or follower")
	}
	if targetUserID == ident.UserID {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_SELF", "cannot change your own role")
	}
	if err := s.store.SetRole(ctx, channelID, targetUserID, role); err != nil {
		if errors.Is(err, ErrNotFound) {
			return httpx.Reject(http.StatusNotFound, "MEMBER_NOT_FOUND", "that user does not follow this channel")
		}
		return httpx.Transient()
	}
	return nil
}

// ── posts ───────────────────────────────────────────────────────────────────

// CreatePost publishes (or schedules) a post (admin+). A future publish_at
// schedules it (published=false until the sweep flips it); an immediate post is
// broadcast now.
func (s *Service) CreatePost(ctx context.Context, ident auth.Identity, channelID, body string, mediaRef *string, publishAtMS int64) (PostView, error) {
	_, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return PostView{}, err
	}
	if !member || !domain.CanPost(me.Role) {
		return PostView{}, s.notFound()
	}
	if err := domain.ValidatePost(body); err != nil {
		return PostView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_POST", err.Error())
	}
	now := s.now()
	publishAt := now
	if publishAtMS > now.UnixMilli() {
		publishAt = time.UnixMilli(publishAtMS)
	}
	published := !publishAt.After(now)
	p := Post{
		ID: s.newID(), ChannelID: channelID, AuthorID: ident.UserID, Body: body,
		MediaRef: mediaRef, PublishAt: publishAt, Published: published, CreatedAt: now,
	}
	if err := s.store.CreatePost(ctx, p); err != nil {
		return PostView{}, httpx.Transient()
	}
	if published {
		s.broadcast(ctx, channelID, p.ID)
	}
	return postView(p, nil, 0), nil
}

// Posts returns a channel's feed (published posts). Public channels are readable
// by anyone; private channels only by members.
func (s *Service) Posts(ctx context.Context, ident auth.Identity, channelID string, limit int) ([]PostView, error) {
	c, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return nil, err
	}
	if !s.canRead(ctx, c, me, member, ident.UserID) {
		if c.Premium {
			return nil, httpx.Reject(http.StatusPaymentRequired, "PREMIUM_REQUIRED", "subscribe to view this channel")
		}
		return nil, s.notFound()
	}
	ps, err := s.store.ListPosts(ctx, channelID, clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]PostView, 0, len(ps))
	for _, p := range ps {
		reactions, _ := s.store.Reactions(ctx, p.ID)
		comments, _ := s.store.CommentCount(ctx, p.ID)
		out = append(out, postView(p, reactions, comments))
	}
	return out, nil
}

// DeletePost removes a post (its author or a channel admin).
func (s *Service) DeletePost(ctx context.Context, ident auth.Identity, postID string) error {
	p, c, me, member, err := s.post(ctx, postID, ident.UserID)
	if err != nil {
		return err
	}
	_ = c
	if p.AuthorID != ident.UserID && (!member || !domain.CanPost(me.Role)) {
		return s.notFoundPost()
	}
	if err := s.store.DeletePost(ctx, postID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// PublishDue flips scheduled posts whose time has come to published and
// broadcasts each. Driven by a ticker in the deployable.
func (s *Service) PublishDue(ctx context.Context) (int, error) {
	posts, err := s.store.PublishDue(ctx, s.now(), 500)
	if err != nil {
		return 0, err
	}
	for _, p := range posts {
		s.broadcast(ctx, p.ChannelID, p.ID)
	}
	return len(posts), nil
}

// ── reactions ───────────────────────────────────────────────────────────────

// React adds an emoji reaction to a post the caller may view (idempotent).
func (s *Service) React(ctx context.Context, ident auth.Identity, postID, emoji string, on bool) error {
	if emoji == "" || len(emoji) > 16 {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_EMOJI", "invalid emoji")
	}
	if _, err := s.viewablePost(ctx, postID, ident.UserID); err != nil {
		return err
	}
	var err error
	if on {
		err = s.store.React(ctx, postID, ident.UserID, emoji)
	} else {
		err = s.store.Unreact(ctx, postID, ident.UserID, emoji)
	}
	if err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── comments ────────────────────────────────────────────────────────────────

// Comment adds a comment to a post the caller may view.
func (s *Service) Comment(ctx context.Context, ident auth.Identity, postID, body string) (CommentView, error) {
	if _, err := s.viewablePost(ctx, postID, ident.UserID); err != nil {
		return CommentView{}, err
	}
	if err := domain.ValidateComment(body); err != nil {
		return CommentView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_COMMENT", err.Error())
	}
	c := Comment{ID: s.newID(), PostID: postID, AuthorID: ident.UserID, Body: body, CreatedAt: s.now()}
	if err := s.store.CreateComment(ctx, c); err != nil {
		return CommentView{}, httpx.Transient()
	}
	return CommentView{ID: c.ID, AuthorID: c.AuthorID, Body: c.Body, CreatedAt: c.CreatedAt.UnixMilli()}, nil
}

// Comments lists a post's comments (viewers of the post).
func (s *Service) Comments(ctx context.Context, ident auth.Identity, postID string, limit int) ([]CommentView, error) {
	if _, err := s.viewablePost(ctx, postID, ident.UserID); err != nil {
		return nil, err
	}
	cs, err := s.store.ListComments(ctx, postID, clampLimit(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]CommentView, 0, len(cs))
	for _, c := range cs {
		out = append(out, CommentView{ID: c.ID, AuthorID: c.AuthorID, Body: c.Body, CreatedAt: c.CreatedAt.UnixMilli()})
	}
	return out, nil
}

// DeleteComment removes a comment (its author or a channel admin).
func (s *Service) DeleteComment(ctx context.Context, ident auth.Identity, commentID string) error {
	cm, err := s.store.GetComment(ctx, commentID)
	if errors.Is(err, ErrNotFound) {
		return httpx.Reject(http.StatusNotFound, "COMMENT_NOT_FOUND", "comment not found")
	}
	if err != nil {
		return httpx.Transient()
	}
	if cm.AuthorID != ident.UserID {
		p, _, me, member, perr := s.post(ctx, cm.PostID, ident.UserID)
		if perr != nil {
			return perr
		}
		_ = p
		if !member || !domain.CanPost(me.Role) {
			return httpx.Reject(http.StatusNotFound, "COMMENT_NOT_FOUND", "comment not found")
		}
	}
	if err := s.store.DeleteComment(ctx, commentID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── analytics + monetization (T7.03) ────────────────────────────────────────

// RecordView bumps a post's aggregate view counter (privacy-preserving — no
// per-viewer row). Any authed user who may see the post may record a view.
func (s *Service) RecordView(ctx context.Context, ident auth.Identity, postID string) error {
	if _, err := s.viewablePost(ctx, postID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.IncrementViews(ctx, postID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// GetInsights returns a channel's aggregate analytics (channel admin+ only).
func (s *Service) GetInsights(ctx context.Context, ident auth.Identity, channelID string) (Insights, error) {
	_, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return Insights{}, err
	}
	if !member || !domain.CanEditInfo(me.Role) {
		return Insights{}, s.notFound()
	}
	ins, err := s.store.Insights(ctx, channelID)
	if err != nil {
		return Insights{}, httpx.Transient()
	}
	ins.ChannelID = channelID
	return ins, nil
}

// SetPremium toggles a channel's premium gate + monthly price (owner only).
func (s *Service) SetPremium(ctx context.Context, ident auth.Identity, channelID string, premium bool, priceCents int) error {
	_, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return err
	}
	if !member || !domain.CanDelete(me.Role) { // owner-only (reuse the owner gate)
		return s.notFound()
	}
	if priceCents < 0 || priceCents > 100_000 {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_PRICE", "price must be 0–100000 cents")
	}
	if err := s.store.SetPremium(ctx, channelID, premium, priceCents); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Subscribe grants the caller premium access for a period. The CHARGE runs
// through the PaymentGateway seam (Noop in dev — no money moves). A subscriber
// also follows the channel.
func (s *Service) Subscribe(ctx context.Context, ident auth.Identity, channelID string) (SubscribeResult, error) {
	c, me, member, err := s.caller(ctx, channelID, ident.UserID)
	if err != nil {
		return SubscribeResult{}, err
	}
	if c.Kind == domain.KindPrivate && !member {
		return SubscribeResult{}, s.notFound()
	}
	if !c.Premium {
		return SubscribeResult{}, httpx.Reject(http.StatusConflict, "NOT_PREMIUM", "this channel is not premium")
	}
	if member && me.Role >= domain.RoleAdmin {
		return SubscribeResult{}, httpx.Reject(http.StatusConflict, "ALREADY_MEMBER", "admins already have access")
	}
	ref, err := s.charge(ctx, ident.UserID, channelID, c.PriceCents)
	if err != nil {
		return SubscribeResult{}, httpx.Reject(http.StatusPaymentRequired, "PAYMENT_FAILED", "the payment could not be completed")
	}
	expires := s.now().Add(subscriptionPeriod)
	if err := s.store.Subscribe(ctx, channelID, ident.UserID, ref, expires); err != nil {
		return SubscribeResult{}, httpx.Transient()
	}
	_ = s.store.AddMember(ctx, Member{ChannelID: channelID, UserID: ident.UserID, Role: domain.RoleFollower, JoinedAt: s.now()})
	return SubscribeResult{PaymentRef: ref, ExpiresAtMS: expires.UnixMilli()}, nil
}

func (s *Service) charge(ctx context.Context, userID, channelID string, cents int) (string, error) {
	if s.payments == nil {
		return "dev-noop", nil
	}
	return s.payments.Charge(ctx, userID, channelID, cents)
}

// canRead reports whether the caller may read a channel's posts: public+free →
// anyone; private → members; premium → admins or active subscribers.
func (s *Service) canRead(ctx context.Context, c Channel, me Member, member bool, userID string) bool {
	if c.Kind == domain.KindPrivate && !member {
		return false
	}
	if c.Premium {
		if member && me.Role >= domain.RoleAdmin {
			return true
		}
		ok, _ := s.store.IsSubscribed(ctx, c.ID, userID, s.now())
		return ok
	}
	return true
}

// ── helpers ─────────────────────────────────────────────────────────────────

// caller loads the channel and the caller's membership (member=false if none).
func (s *Service) caller(ctx context.Context, channelID, userID string) (Channel, Member, bool, error) {
	c, err := s.store.GetChannel(ctx, channelID)
	if errors.Is(err, ErrNotFound) {
		return Channel{}, Member{}, false, s.notFound()
	}
	if err != nil {
		return Channel{}, Member{}, false, httpx.Transient()
	}
	m, err := s.store.GetMember(ctx, channelID, userID)
	if errors.Is(err, ErrNotFound) {
		return c, Member{}, false, nil
	}
	if err != nil {
		return Channel{}, Member{}, false, httpx.Transient()
	}
	return c, m, true, nil
}

// post loads a post + its channel + the caller's membership.
func (s *Service) post(ctx context.Context, postID, userID string) (Post, Channel, Member, bool, error) {
	p, err := s.store.GetPost(ctx, postID)
	if errors.Is(err, ErrNotFound) {
		return Post{}, Channel{}, Member{}, false, s.notFoundPost()
	}
	if err != nil {
		return Post{}, Channel{}, Member{}, false, httpx.Transient()
	}
	c, me, member, err := s.caller(ctx, p.ChannelID, userID)
	if err != nil {
		return Post{}, Channel{}, Member{}, false, err
	}
	return p, c, me, member, nil
}

// viewablePost checks the caller may see a post (public channel or member) and
// returns it.
func (s *Service) viewablePost(ctx context.Context, postID, userID string) (Post, error) {
	p, c, me, member, err := s.post(ctx, postID, userID)
	if err != nil {
		return Post{}, err
	}
	if !p.Published || !s.canRead(ctx, c, me, member, userID) {
		return Post{}, s.notFoundPost()
	}
	return p, nil
}

func (s *Service) view(ctx context.Context, c Channel, myRole domain.Role, member bool, userID string) ChannelView {
	followers, _ := s.store.FollowerCount(ctx, c.ID)
	v := ChannelView{
		ID: c.ID, Handle: c.Handle, Name: c.Name, Description: c.Description,
		Kind: c.Kind.String(), Verified: c.Verified, Followers: followers,
		Premium: c.Premium, PriceCents: c.PriceCents, CreatedAt: c.CreatedAt.UnixMilli(),
	}
	if member {
		v.MyRole = myRole.String()
	}
	// The owner/admins always have premium access; others need a subscription.
	if myRole >= domain.RoleAdmin && member {
		v.MySubscribed = true
	} else if c.Premium {
		v.MySubscribed, _ = s.store.IsSubscribed(ctx, c.ID, userID, s.now())
	}
	return v
}

func (s *Service) publicViews(ctx context.Context, cs []Channel) []ChannelView {
	out := make([]ChannelView, 0, len(cs))
	for _, c := range cs {
		followers, _ := s.store.FollowerCount(ctx, c.ID)
		out = append(out, ChannelView{
			ID: c.ID, Handle: c.Handle, Name: c.Name, Description: c.Description,
			Kind: c.Kind.String(), Verified: c.Verified, Followers: followers,
			Premium: c.Premium, PriceCents: c.PriceCents, CreatedAt: c.CreatedAt.UnixMilli(),
		})
	}
	return out
}

func (s *Service) broadcast(ctx context.Context, channelID, postID string) {
	if s.broadcaster == nil {
		return
	}
	if err := s.broadcaster.PostPublished(ctx, channelID, postID); err != nil && s.log != nil {
		s.log.Warn("channels: broadcast failed", "channel", channelID, "post", postID, "err", err)
	}
}

func (s *Service) notFound() error {
	return httpx.Reject(http.StatusNotFound, "CHANNEL_NOT_FOUND", "channel not found")
}
func (s *Service) notFoundPost() error {
	return httpx.Reject(http.StatusNotFound, "POST_NOT_FOUND", "post not found")
}

func postView(p Post, reactions map[string]int, comments int) PostView {
	v := PostView{
		ID: p.ID, ChannelID: p.ChannelID, Body: p.Body, PublishAt: p.PublishAt.UnixMilli(),
		Reactions: reactions, Comments: comments, CreatedAt: p.CreatedAt.UnixMilli(),
	}
	if p.MediaRef != nil {
		v.MediaRef = *p.MediaRef
	}
	if !p.Published {
		v.Scheduled = true
	}
	return v
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}
