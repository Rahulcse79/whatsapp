// Package contacts owns hashed address-book sync (peppered HMAC discovery),
// username search, favorites, and invite-a-friend links. Plaintext address books
// never reach the server at rest — only the peppered HMAC is stored (FR-CONT-01);
// enumeration defenses per threat-model T11.
package contacts

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/contacts/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// ErrNotFound is returned by ports when no row matches.
var ErrNotFound = errors.New("contacts: not found")

// Match is a discovered contact: which peppered hash matched, and the user.
type Match struct {
	Hash     []byte
	UserID   string
	Username string
}

// UserRef is a metadata-only user reference (search / matched contact).
type UserRef struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// MatchedContact is one sync hit, echoing the caller's handle so the client can
// map it back to its address-book entry.
type MatchedContact struct {
	Handle   string `json:"handle"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// ContactEdge is a persisted owner→user edge (peppered hash, matched user).
type ContactEdge struct {
	Hash          []byte
	MatchedUserID string
}

// InviteLink is the create-invite response.
type InviteLink struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at_ms"`
	MaxUses   int    `json:"max_uses"`
}

// InviteInfo is the (public) resolve-invite response.
type InviteInfo struct {
	InviterID string `json:"inviter_id"`
	ExpiresAt int64  `json:"expires_at_ms"`
	Uses      int    `json:"uses"`
	MaxUses   int    `json:"max_uses"`
}

// ── ports ────────────────────────────────────────────────────────────────

// Hasher peppers a phone handle into its discovery hash. Implemented by the auth
// context (auth.Service.PhoneHash) so registration and sync key identically —
// one definition of the peppered HMAC.
type Hasher interface {
	PhoneHash(handle string) []byte
}

// Directory resolves discovery hashes and username searches against the user
// registry (read-only, metadata-only).
type Directory interface {
	// MatchHashes returns a match for each peppered hash that belongs to a
	// registered, discoverable user (subset/order unspecified).
	MatchHashes(ctx context.Context, hashes [][]byte) ([]Match, error)
	// SearchUsername returns up to limit users whose username matches (PG trigram).
	SearchUsername(ctx context.Context, query string, limit int) ([]UserRef, error)
}

// ContactStore persists an owner's matched contact edges (peppered hashes only).
type ContactStore interface {
	UpsertMatched(ctx context.Context, ownerID string, edges []ContactEdge) error
}

// Favorites is the per-user favorite set (keyed by target user id).
type Favorites interface {
	Add(ctx context.Context, ownerID, targetID string) error // ErrNotFound if target unknown
	Remove(ctx context.Context, ownerID, targetID string) error
	List(ctx context.Context, ownerID string) ([]UserRef, error)
}

// Invites persists invite-a-friend capability tokens.
type Invites interface {
	Create(ctx context.Context, inv domain.Invite) error
	Get(ctx context.Context, token string) (domain.Invite, error) // ErrNotFound
	Revoke(ctx context.Context, inviterID, token string) error    // ErrNotFound if not owner/unknown
}

// DailyLimiter enforces a per-key daily cap (contact-sync 4/day, T11).
type DailyLimiter interface {
	// AllowDaily records one use of key for the current day and reports whether
	// it stays within limit.
	AllowDaily(ctx context.Context, key string, limit int) (bool, error)
}

// Rate is a GCRA gate (username search).
type Rate interface {
	Allow(ctx context.Context, key string) (bool, error)
}

// Service orchestrates sync, search, favorites, and invites.
type Service struct {
	hasher      Hasher
	directory   Directory
	contacts    ContactStore
	favorites   Favorites
	invites     Invites
	syncLimiter DailyLimiter
	searchRate  Rate
	inviteBase  string
	log         *slog.Logger
	now         func() time.Time
	newToken    func() string
}

func NewService(hasher Hasher, dir Directory, store ContactStore, fav Favorites, inv Invites, syncLimiter DailyLimiter, searchRate Rate, inviteBase string, log *slog.Logger) *Service {
	return &Service{
		hasher: hasher, directory: dir, contacts: store, favorites: fav, invites: inv,
		syncLimiter: syncLimiter, searchRate: searchRate, inviteBase: inviteBase, log: log,
		now: time.Now, newToken: randomToken,
	}
}

// Sync discovers which of the caller's address-book handles belong to registered
// users. Enumeration defenses (T11): ≤ 5k handles/call, 4/day, and uniform
// per-handle work (every handle is hashed and matched in one batch — no early
// exit — so timing does not reveal which handles matched). Only peppered hashes
// are persisted; plaintext handles are never stored.
func (s *Service) Sync(ctx context.Context, ident auth.Identity, handles []string) ([]MatchedContact, error) {
	if len(handles) == 0 {
		return nil, httpx.Reject(http.StatusBadRequest, "VALIDATION_EMPTY", "no handles supplied")
	}
	if len(handles) > domain.MaxSyncHandles {
		return nil, httpx.Reject(http.StatusRequestEntityTooLarge, "VALIDATION_TOO_MANY", "too many handles (max 5000)")
	}
	if ok, err := s.syncLimiter.AllowDaily(ctx, "contact-sync:"+ident.UserID, domain.SyncDailyLimit); err != nil {
		return nil, httpx.Transient()
	} else if !ok {
		return nil, httpx.Reject(http.StatusTooManyRequests, "RATE_LIMITED", "contact sync limited to 4/day")
	}

	// Peppered hash for every well-formed handle (uniform work; dedup identical).
	handleByHash := make(map[string]string, len(handles))
	hashes := make([][]byte, 0, len(handles))
	for _, h := range handles {
		norm, valid := domain.NormalizeHandle(h)
		if !valid {
			continue // malformed → cannot match; contributes no timing signal
		}
		ph := s.hasher.PhoneHash(norm)
		key := string(ph)
		if _, dup := handleByHash[key]; dup {
			continue
		}
		handleByHash[key] = norm
		hashes = append(hashes, ph)
	}

	matches, err := s.directory.MatchHashes(ctx, hashes)
	if err != nil {
		return nil, httpx.Transient()
	}

	edges := make([]ContactEdge, 0, len(matches))
	out := make([]MatchedContact, 0, len(matches))
	for _, m := range matches {
		edges = append(edges, ContactEdge{Hash: m.Hash, MatchedUserID: m.UserID})
		out = append(out, MatchedContact{Handle: handleByHash[string(m.Hash)], UserID: m.UserID, Username: m.Username})
	}
	if len(edges) > 0 {
		// Persisting the social graph is secondary to discovery; a store hiccup
		// must not fail the sync the client already sees results for.
		if err := s.contacts.UpsertMatched(ctx, ident.UserID, edges); err != nil {
			s.log.Warn("contacts: persisting matched edges failed", "owner", ident.UserID, "err", err)
		}
	}
	return out, nil
}

// Search finds users by username (metadata only, PG trigram), rate-limited and
// length-guarded. The caller is excluded from results.
func (s *Service) Search(ctx context.Context, ident auth.Identity, query string) ([]UserRef, error) {
	norm, ok := domain.NormalizeQuery(query)
	if !ok {
		return nil, httpx.Reject(http.StatusBadRequest, "VALIDATION_QUERY", "query must be at least 2 characters")
	}
	if ok, err := s.searchRate.Allow(ctx, "contact-search:"+ident.UserID); err != nil {
		return nil, httpx.Transient()
	} else if !ok {
		return nil, httpx.Reject(http.StatusTooManyRequests, "RATE_LIMITED", "too many searches")
	}
	refs, err := s.directory.SearchUsername(ctx, norm, domain.MaxSearchResults)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := refs[:0]
	for _, r := range refs {
		if r.UserID != ident.UserID {
			out = append(out, r)
		}
	}
	return out, nil
}

// AddFavorite marks a user as a favorite (idempotent). Favoriting yourself is
// rejected; an unknown target is 404.
func (s *Service) AddFavorite(ctx context.Context, ident auth.Identity, targetID string) error {
	if targetID == "" || targetID == ident.UserID {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_TARGET", "invalid favorite target")
	}
	if err := s.favorites.Add(ctx, ident.UserID, targetID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return httpx.Reject(http.StatusNotFound, "USER_NOT_FOUND", "no such user")
		}
		return httpx.Transient()
	}
	return nil
}

// RemoveFavorite unmarks a favorite (idempotent).
func (s *Service) RemoveFavorite(ctx context.Context, ident auth.Identity, targetID string) error {
	if err := s.favorites.Remove(ctx, ident.UserID, targetID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ListFavorites returns the caller's favorites.
func (s *Service) ListFavorites(ctx context.Context, ident auth.Identity) ([]UserRef, error) {
	refs, err := s.favorites.List(ctx, ident.UserID)
	if err != nil {
		return nil, httpx.Transient()
	}
	return refs, nil
}

// CreateInvite mints a personal invite-a-friend link (expiry + max-uses +
// revocable).
func (s *Service) CreateInvite(ctx context.Context, ident auth.Identity) (InviteLink, error) {
	inv := domain.Invite{
		Token:     s.newToken(),
		InviterID: ident.UserID,
		ExpiresAt: s.now().Add(domain.InviteTTL),
		MaxUses:   domain.DefaultInviteMaxUses,
	}
	if err := s.invites.Create(ctx, inv); err != nil {
		return InviteLink{}, httpx.Transient()
	}
	return InviteLink{
		Token:     inv.Token,
		URL:       s.inviteBase + "/i/" + inv.Token,
		ExpiresAt: inv.ExpiresAt.UnixMilli(),
		MaxUses:   inv.MaxUses,
	}, nil
}

// ResolveInvite returns the (public) inviter info for a token, or 404/410. Used
// by the landing page a shared link opens — no auth (the token is the capability).
func (s *Service) ResolveInvite(ctx context.Context, token string) (InviteInfo, error) {
	inv, err := s.invites.Get(ctx, token)
	if errors.Is(err, ErrNotFound) {
		return InviteInfo{}, httpx.Reject(http.StatusNotFound, "INVITE_NOT_FOUND", "no such invite")
	}
	if err != nil {
		return InviteInfo{}, httpx.Transient()
	}
	if !inv.Valid(s.now()) {
		return InviteInfo{}, httpx.Reject(http.StatusGone, "INVITE_EXPIRED", "this invite is no longer valid")
	}
	return InviteInfo{InviterID: inv.InviterID, ExpiresAt: inv.ExpiresAt.UnixMilli(), Uses: inv.Uses, MaxUses: inv.MaxUses}, nil
}

// RevokeInvite revokes one of the caller's invites (idempotent-ish: unknown or
// not-owned → 404).
func (s *Service) RevokeInvite(ctx context.Context, ident auth.Identity, token string) error {
	if err := s.invites.Revoke(ctx, ident.UserID, token); err != nil {
		if errors.Is(err, ErrNotFound) {
			return httpx.Reject(http.StatusNotFound, "INVITE_NOT_FOUND", "no such invite")
		}
		return httpx.Transient()
	}
	return nil
}

// randomToken is an unguessable URL-safe capability token (128-bit).
func randomToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// OS entropy failure is unrecoverable — fall back to a UUIDv7 (still random).
		return id.New()
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
