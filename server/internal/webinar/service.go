package webinar

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
	"github.com/whatsapp-v2/server/internal/webinar/domain"
)

// Service runs the webinar control plane: create, waiting-room admission,
// raise-hand, promote/demote, roster + attendance, and Q&A.
type Service struct {
	store  Store
	minter Minter
	now    func() time.Time
	newID  func() string
}

func NewService(store Store, minter Minter) *Service {
	return &Service{store: store, minter: minter, now: time.Now, newID: id.New}
}

// Create starts a webinar with the caller as host (admitted, publishing) and
// returns the host's join token.
func (s *Service) Create(ctx context.Context, ident auth.Identity, title string) (CreateResult, error) {
	if err := domain.ValidateCreate(title); err != nil {
		return CreateResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_TITLE", err.Error())
	}
	now := s.now()
	w := Webinar{ID: s.newID(), Title: strings.TrimSpace(title), HostID: ident.UserID, RoomID: "wbn-" + s.newID(), CreatedAt: now}
	if err := s.store.Create(ctx, w); err != nil {
		return CreateResult{}, httpx.Transient()
	}
	host := Participant{UserID: ident.UserID, Role: domain.RoleHost, Status: domain.StatusAdmitted, JoinedAt: now}
	if err := s.store.UpsertParticipant(ctx, w.ID, host); err != nil {
		return CreateResult{}, httpx.Transient()
	}
	token, err := s.minter.MintJoin(w.RoomID, ident.UserID, true)
	if err != nil {
		return CreateResult{}, httpx.Transient()
	}
	return CreateResult{WebinarID: w.ID, RoomID: w.RoomID, JoinToken: token}, nil
}

// Join puts the caller in the waiting room (attendee) unless they're already a
// participant. The host admits from the roster; the attendee then polls Me for a
// token. (A future "open" webinar could auto-admit — waiting-room is the default.)
func (s *Service) Join(ctx context.Context, ident auth.Identity, webinarID string) (MeResult, error) {
	w, err := s.load(ctx, webinarID)
	if err != nil {
		return MeResult{}, err
	}
	if w.EndedAt != nil {
		return MeResult{}, httpx.Reject(http.StatusConflict, "WEBINAR_ENDED", "this webinar has ended")
	}
	if _, err := s.store.GetParticipant(ctx, webinarID, ident.UserID); errors.Is(err, ErrNotFound) {
		p := Participant{UserID: ident.UserID, Role: domain.RoleAttendee, Status: domain.StatusWaiting, JoinedAt: s.now()}
		if err := s.store.UpsertParticipant(ctx, webinarID, p); err != nil {
			return MeResult{}, httpx.Transient()
		}
	}
	return s.Me(ctx, ident, webinarID)
}

// Me returns the caller's live state + a role-scoped join token once admitted.
func (s *Service) Me(ctx context.Context, ident auth.Identity, webinarID string) (MeResult, error) {
	w, err := s.load(ctx, webinarID)
	if err != nil {
		return MeResult{}, err
	}
	p, err := s.participant(ctx, webinarID, ident.UserID)
	if err != nil {
		return MeResult{}, err
	}
	res := MeResult{Status: p.Status.String(), Role: p.Role.String(), HandRaised: p.HandRaised, RoomID: w.RoomID, CanPublish: domain.CanPublish(p.Role)}
	if p.Status == domain.StatusAdmitted {
		token, err := s.minter.MintJoin(w.RoomID, ident.UserID, domain.CanPublish(p.Role))
		if err != nil {
			return MeResult{}, httpx.Transient()
		}
		res.JoinToken = token
	}
	return res, nil
}

// Admit / Deny move a waiting attendee (host-only).
func (s *Service) Admit(ctx context.Context, ident auth.Identity, webinarID, userID string) error {
	if err := s.requireHost(ctx, webinarID, ident.UserID); err != nil {
		return err
	}
	return s.setStatus(ctx, webinarID, userID, domain.StatusAdmitted, nil)
}

func (s *Service) Deny(ctx context.Context, ident auth.Identity, webinarID, userID string) error {
	if err := s.requireHost(ctx, webinarID, ident.UserID); err != nil {
		return err
	}
	now := s.now()
	return s.setStatus(ctx, webinarID, userID, domain.StatusLeft, &now)
}

// RaiseHand / LowerHand toggle the caller's hand (admitted attendees).
func (s *Service) SetHand(ctx context.Context, ident auth.Identity, webinarID string, raised bool) error {
	if _, err := s.participant(ctx, webinarID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.SetHand(ctx, webinarID, ident.UserID, raised); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Promote / Demote change an attendee↔speaker (host-only). The target re-fetches
// Me to get a token with the new publish grant.
func (s *Service) SetRole(ctx context.Context, ident auth.Identity, webinarID, userID, roleStr string) error {
	if err := s.requireHost(ctx, webinarID, ident.UserID); err != nil {
		return err
	}
	var role domain.Role
	switch roleStr {
	case "speaker":
		role = domain.RoleSpeaker
	case "attendee":
		role = domain.RoleAttendee
	default:
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_ROLE", "role must be speaker or attendee")
	}
	if userID == ident.UserID {
		return httpx.Reject(http.StatusConflict, "STATE_SELF_ROLE", "the host can't change their own role")
	}
	if _, err := s.participant(ctx, webinarID, userID); err != nil {
		return err
	}
	if err := s.store.SetRole(ctx, webinarID, userID, role); err != nil {
		return httpx.Transient()
	}
	// Promoting clears the raised hand.
	if role == domain.RoleSpeaker {
		_ = s.store.SetHand(ctx, webinarID, userID, false)
	}
	return nil
}

// Leave marks the caller as left (records attendance end).
func (s *Service) Leave(ctx context.Context, ident auth.Identity, webinarID string) error {
	if _, err := s.store.GetParticipant(ctx, webinarID, ident.UserID); errors.Is(err, ErrNotFound) {
		return nil
	}
	now := s.now()
	return s.setStatus(ctx, webinarID, ident.UserID, domain.StatusLeft, &now)
}

// Roster is the host's live list (waiting + admitted + hands) — also the
// attendance report (join/leave times). Host-only.
func (s *Service) Roster(ctx context.Context, ident auth.Identity, webinarID string) ([]RosterEntry, error) {
	if err := s.requireHost(ctx, webinarID, ident.UserID); err != nil {
		return nil, err
	}
	ps, err := s.store.ListParticipants(ctx, webinarID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]RosterEntry, len(ps))
	for i, p := range ps {
		e := RosterEntry{UserID: p.UserID, Role: p.Role.String(), Status: p.Status.String(), HandRaised: p.HandRaised, JoinedAtMS: p.JoinedAt.UnixMilli()}
		if p.LeftAt != nil {
			e.LeftAtMS = p.LeftAt.UnixMilli()
		}
		out[i] = e
	}
	return out, nil
}

// End closes the webinar (host-only).
func (s *Service) End(ctx context.Context, ident auth.Identity, webinarID string) error {
	if err := s.requireHost(ctx, webinarID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.End(ctx, webinarID, s.now()); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── Q&A ──────────────────────────────────────────────────────────────────────

func (s *Service) AskQuestion(ctx context.Context, ident auth.Identity, webinarID, body string) (QuestionView, error) {
	if _, err := s.participant(ctx, webinarID, ident.UserID); err != nil {
		return QuestionView{}, err
	}
	if err := domain.ValidateQuestion(body); err != nil {
		return QuestionView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_QUESTION", err.Error())
	}
	q := Question{ID: s.newID(), AskerID: ident.UserID, Body: strings.TrimSpace(body), CreatedAt: s.now()}
	if err := s.store.CreateQuestion(ctx, webinarID, q); err != nil {
		return QuestionView{}, httpx.Transient()
	}
	return questionView(q), nil
}

func (s *Service) Questions(ctx context.Context, ident auth.Identity, webinarID string) ([]QuestionView, error) {
	if _, err := s.participant(ctx, webinarID, ident.UserID); err != nil {
		return nil, err
	}
	qs, err := s.store.ListQuestions(ctx, webinarID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]QuestionView, len(qs))
	for i, q := range qs {
		out[i] = questionView(q)
	}
	return out, nil
}

func (s *Service) UpvoteQuestion(ctx context.Context, ident auth.Identity, webinarID, questionID string) error {
	if _, err := s.participant(ctx, webinarID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.UpvoteQuestion(ctx, webinarID, questionID, ident.UserID); err != nil {
		return httpx.Transient()
	}
	return nil
}

func (s *Service) AnswerQuestion(ctx context.Context, ident auth.Identity, webinarID, questionID string) error {
	if err := s.requireHost(ctx, webinarID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.AnswerQuestion(ctx, webinarID, questionID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── helpers ────────────────────────────────────────────────────────────────

func (s *Service) load(ctx context.Context, webinarID string) (Webinar, error) {
	w, err := s.store.Get(ctx, webinarID)
	if errors.Is(err, ErrNotFound) {
		return Webinar{}, notFound()
	}
	if err != nil {
		return Webinar{}, httpx.Transient()
	}
	return w, nil
}

func (s *Service) participant(ctx context.Context, webinarID, userID string) (Participant, error) {
	p, err := s.store.GetParticipant(ctx, webinarID, userID)
	if errors.Is(err, ErrNotFound) {
		return Participant{}, notFound()
	}
	if err != nil {
		return Participant{}, httpx.Transient()
	}
	return p, nil
}

func (s *Service) requireHost(ctx context.Context, webinarID, userID string) error {
	if _, err := s.load(ctx, webinarID); err != nil {
		return err
	}
	p, err := s.store.GetParticipant(ctx, webinarID, userID)
	if err != nil || !domain.CanHost(p.Role) {
		return notFound() // don't reveal the webinar to non-hosts probing host actions
	}
	return nil
}

func (s *Service) setStatus(ctx context.Context, webinarID, userID string, status domain.Status, leftAt *time.Time) error {
	if _, err := s.participant(ctx, webinarID, userID); err != nil {
		return err
	}
	if err := s.store.SetStatus(ctx, webinarID, userID, status, leftAt); err != nil {
		return httpx.Transient()
	}
	return nil
}

func questionView(q Question) QuestionView {
	return QuestionView{ID: q.ID, AskerID: q.AskerID, Body: q.Body, Upvotes: q.Upvotes, Answered: q.Answered, CreatedAtMS: q.CreatedAt.UnixMilli()}
}

func notFound() error {
	return httpx.Reject(http.StatusNotFound, "WEBINAR_NOT_FOUND", "webinar not found")
}
