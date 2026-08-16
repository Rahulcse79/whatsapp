package deviceauth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/deviceauth/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

const (
	challengeTTL    = 5 * time.Minute
	maxCredsPerUser = 20
	recentLoginsMax = 50
)

// Service runs the passkey ceremonies + the login-event audit.
type Service struct {
	store  Store
	rpID   string
	origin string
	now    func() time.Time
	newID  func() string
}

func NewService(store Store, rpID, origin string) *Service {
	return &Service{store: store, rpID: rpID, origin: origin, now: time.Now, newID: id.New}
}

// ── passkey registration ─────────────────────────────────────────────────────

// BeginRegistration issues a challenge for enrolling a new passkey.
func (s *Service) BeginRegistration(ctx context.Context, ident auth.Identity) (RegistrationOptions, error) {
	ch, err := s.issue(ctx, ident.UserID, "register")
	if err != nil {
		return RegistrationOptions{}, err
	}
	return RegistrationOptions{Challenge: ch, RPID: s.rpID, Origin: s.origin, UserID: ident.UserID, Algs: []int{domain.AlgES256, domain.AlgEdDSA}}, nil
}

// FinishRegistrationInput carries the browser's create() result (the client
// extracts the credential public key from attestationObject; "none" attestation).
type FinishRegistrationInput struct {
	CredentialID   string
	Alg            int
	PublicKeyB64   string // base64url; ES256 x||y (64B) or EdDSA (32B)
	ClientDataJSON string // base64url
	Name           string
}

// FinishRegistration verifies the ceremony challenge and stores the passkey.
func (s *Service) FinishRegistration(ctx context.Context, ident auth.Identity, in FinishRegistrationInput) error {
	cd, err := s.consumeClientData(ctx, ident.UserID, "register", "webauthn.create", in.ClientDataJSON)
	if err != nil {
		return err
	}
	_ = cd
	if in.Alg != domain.AlgES256 && in.Alg != domain.AlgEdDSA {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_ALG", "unsupported passkey algorithm")
	}
	pub, err := domain.DecodeB64URL(in.PublicKeyB64)
	if err != nil || !validKeyLen(in.Alg, len(pub)) {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_KEY", "malformed public key")
	}
	if strings.TrimSpace(in.CredentialID) == "" {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_CRED", "missing credential id")
	}
	existing, err := s.store.ListCredentials(ctx, ident.UserID)
	if err != nil {
		return httpx.Transient()
	}
	if len(existing) >= maxCredsPerUser {
		return httpx.Reject(http.StatusConflict, "STATE_TOO_MANY_KEYS", "too many passkeys; remove one first")
	}
	cred := Credential{ID: in.CredentialID, UserID: ident.UserID, Alg: in.Alg, PublicKey: pub, Name: nameOr(in.Name), CreatedAt: s.now()}
	if err := s.store.CreateCredential(ctx, cred); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── passkey assertion (2FA / step-up) ────────────────────────────────────────

// BeginLogin issues a challenge + the caller's allowed credential ids.
func (s *Service) BeginLogin(ctx context.Context, ident auth.Identity) (LoginOptions, error) {
	creds, err := s.store.ListCredentials(ctx, ident.UserID)
	if err != nil {
		return LoginOptions{}, httpx.Transient()
	}
	ch, err := s.issue(ctx, ident.UserID, "login")
	if err != nil {
		return LoginOptions{}, err
	}
	ids := make([]string, len(creds))
	for i, c := range creds {
		ids[i] = c.ID
	}
	return LoginOptions{Challenge: ch, RPID: s.rpID, Origin: s.origin, AllowedCredIDs: ids}, nil
}

// FinishLoginInput carries the browser's get() assertion.
type FinishLoginInput struct {
	CredentialID      string
	AuthenticatorData string // base64url
	ClientDataJSON    string // base64url
	Signature         string // base64url
}

// FinishLogin verifies a passkey assertion. Returns nil on success (the caller
// treats it as a satisfied 2FA / step-up factor).
func (s *Service) FinishLogin(ctx context.Context, ident auth.Identity, in FinishLoginInput) error {
	if _, err := s.consumeClientData(ctx, ident.UserID, "login", "webauthn.get", in.ClientDataJSON); err != nil {
		return err
	}
	cred, err := s.store.GetCredential(ctx, in.CredentialID)
	if errors.Is(err, ErrNotFound) || (err == nil && cred.UserID != ident.UserID) {
		return httpx.Reject(http.StatusUnauthorized, "AUTH_PASSKEY", "unknown credential")
	}
	if err != nil {
		return httpx.Transient()
	}
	authData, e1 := domain.DecodeB64URL(in.AuthenticatorData)
	clientData, e2 := domain.DecodeB64URL(in.ClientDataJSON)
	sig, e3 := domain.DecodeB64URL(in.Signature)
	if e1 != nil || e2 != nil || e3 != nil {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_ASSERTION", "malformed assertion")
	}
	ad, err := domain.ParseAuthData(authData)
	if err != nil {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_ASSERTION", err.Error())
	}
	if err := domain.CheckAuthData(ad, s.rpID); err != nil {
		return httpx.Reject(http.StatusUnauthorized, "AUTH_PASSKEY", "authenticator check failed")
	}
	if err := domain.VerifyAssertion(toDomainCred(cred), authData, clientData, sig); err != nil {
		return httpx.Reject(http.StatusUnauthorized, "AUTH_PASSKEY", "assertion verification failed")
	}
	next, err := domain.NextSignCount(cred.SignCount, ad.SignCount)
	if err != nil {
		return httpx.Reject(http.StatusUnauthorized, "AUTH_PASSKEY_CLONE", err.Error())
	}
	_ = s.store.UpdateSignCount(ctx, cred.ID, next, s.now())
	return nil
}

// ListPasskeys returns the caller's registered passkeys.
func (s *Service) ListPasskeys(ctx context.Context, ident auth.Identity) ([]PasskeyView, error) {
	creds, err := s.store.ListCredentials(ctx, ident.UserID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]PasskeyView, len(creds))
	for i, c := range creds {
		v := PasskeyView{ID: c.ID, Name: c.Name, CreatedAtMS: c.CreatedAt.UnixMilli()}
		if c.LastUsedAt != nil {
			v.LastUsedAtMS = c.LastUsedAt.UnixMilli()
		}
		out[i] = v
	}
	return out, nil
}

// DeletePasskey removes one of the caller's passkeys.
func (s *Service) DeletePasskey(ctx context.Context, ident auth.Identity, credID string) error {
	if err := s.store.DeleteCredential(ctx, ident.UserID, credID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── login audit ──────────────────────────────────────────────────────────────

// Observe records a sign-in and flags it if the IP is new for the user. Called
// from the auth verify handlers (best-effort; never blocks the login).
func (s *Service) Observe(ctx context.Context, userID, deviceID, ip, userAgent string) {
	if userID == "" {
		return
	}
	known, _ := s.store.KnownIPs(ctx, userID)
	e := LoginEvent{
		ID: s.newID(), UserID: userID, DeviceID: deviceID, IP: ip, UserAgent: userAgent,
		At: s.now(), Suspicious: domain.IsSuspiciousLogin(known, ip),
	}
	_ = s.store.RecordLogin(ctx, e)
}

// RecentLogins is the security surface: the caller's recent sign-ins.
func (s *Service) RecentLogins(ctx context.Context, ident auth.Identity) ([]LoginView, error) {
	events, err := s.store.RecentLogins(ctx, ident.UserID, recentLoginsMax)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]LoginView, len(events))
	for i, e := range events {
		out[i] = LoginView{DeviceID: e.DeviceID, IP: e.IP, UserAgent: e.UserAgent, AtMS: e.At.UnixMilli(), Suspicious: e.Suspicious}
	}
	return out, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func (s *Service) issue(ctx context.Context, userID, purpose string) (string, error) {
	ch, err := domain.GenChallenge()
	if err != nil {
		return "", httpx.Transient()
	}
	if err := s.store.SaveChallenge(ctx, Challenge{Value: ch, UserID: userID, Purpose: purpose, ExpiresAt: s.now().Add(challengeTTL)}); err != nil {
		return "", httpx.Transient()
	}
	return ch, nil
}

// consumeClientData decodes + single-use-validates the challenge inside a
// clientDataJSON and checks type/origin.
func (s *Service) consumeClientData(ctx context.Context, userID, purpose, wantType, clientDataB64 string) (domain.ClientData, error) {
	raw, err := domain.DecodeB64URL(clientDataB64)
	if err != nil {
		return domain.ClientData{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_CLIENTDATA", "malformed clientDataJSON")
	}
	cd, err := domain.ParseClientData(raw)
	if err != nil {
		return domain.ClientData{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_CLIENTDATA", err.Error())
	}
	ch, err := s.store.TakeChallenge(ctx, cd.Challenge)
	if errors.Is(err, ErrNotFound) || (err == nil && (ch.UserID != userID || ch.Purpose != purpose)) {
		return domain.ClientData{}, httpx.Reject(http.StatusUnauthorized, "AUTH_CHALLENGE", "unknown or mismatched challenge")
	}
	if err != nil {
		return domain.ClientData{}, httpx.Transient()
	}
	if s.now().After(ch.ExpiresAt) {
		return domain.ClientData{}, httpx.Reject(http.StatusUnauthorized, "AUTH_CHALLENGE_EXPIRED", "challenge expired")
	}
	if err := domain.VerifyClientData(cd, wantType, ch.Value, s.origin); err != nil {
		return domain.ClientData{}, httpx.Reject(http.StatusUnauthorized, "AUTH_CLIENTDATA", err.Error())
	}
	return cd, nil
}

func toDomainCred(c Credential) domain.Credential {
	if c.Alg == domain.AlgEdDSA {
		return domain.Credential{Alg: c.Alg, Ed: c.PublicKey}
	}
	return domain.Credential{Alg: c.Alg, X: c.PublicKey[:32], Y: c.PublicKey[32:64]}
}

func validKeyLen(alg, n int) bool {
	if alg == domain.AlgEdDSA {
		return n == 32
	}
	return n == 64 // ES256 x||y
}

func nameOr(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return "Passkey"
	}
	if len(n) > 60 {
		return n[:60]
	}
	return n
}
