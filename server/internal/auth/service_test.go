package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth/domain"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
)

// ─────────────────────────────── fakes ──────────────────────────────────

type fakeChallenges struct {
	mu sync.Mutex
	m  map[string]domain.Challenge
}

func (f *fakeChallenges) Create(_ context.Context, ch domain.Challenge) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[ch.ID] = ch
	return nil
}
func (f *fakeChallenges) Get(_ context.Context, id string) (domain.Challenge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch, ok := f.m[id]
	if !ok {
		return domain.Challenge{}, ErrNotFound
	}
	return ch, nil
}
func (f *fakeChallenges) IncrementAttempts(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := f.m[id]
	ch.Attempts++
	f.m[id] = ch
	return nil
}
func (f *fakeChallenges) MarkVerified(_ context.Context, id string, pinPending bool, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := f.m[id]
	ch.VerifiedAt = at
	ch.PinPending = pinPending
	f.m[id] = ch
	return nil
}

type fakeUsers struct {
	mu sync.Mutex
	m  map[string]User // key: hex phone hash — but string([]byte) works
}

func (f *fakeUsers) ByPhoneHash(_ context.Context, ph []byte) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.m[string(ph)]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}
func (f *fakeUsers) ByID(_ context.Context, uid string) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.m {
		if u.ID == uid {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}
func (f *fakeUsers) SetPINHash(_ context.Context, uid, phc string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, u := range f.m {
		if u.ID == uid {
			u.PINHash = phc
			f.m[k] = u
			return nil
		}
	}
	return ErrNotFound
}

type fakeSessions struct {
	mu sync.Mutex
	m  map[string]domain.Session // by session id
}

func (f *fakeSessions) add(s domain.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[s.ID] = s
}
func (f *fakeSessions) Create(_ context.Context, s domain.Session) error {
	f.add(s)
	return nil
}
func (f *fakeSessions) ByRefreshHash(_ context.Context, h []byte) (domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.m {
		if string(s.RefreshHash) == string(h) {
			return s, nil
		}
	}
	return domain.Session{}, ErrNotFound
}
func (f *fakeSessions) ByRotatedFrom(_ context.Context, h []byte) (domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.m {
		if s.RotatedFrom != nil && string(s.RotatedFrom) == string(h) && s.RevokedAt.IsZero() {
			return s, nil
		}
	}
	return domain.Session{}, ErrNotFound
}
func (f *fakeSessions) Rotate(_ context.Context, id string, oldH, newH []byte, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.m[id]
	if !ok || string(s.RefreshHash) != string(oldH) || !s.RevokedAt.IsZero() {
		return false, nil
	}
	s.RotatedFrom = oldH
	s.RefreshHash = newH
	f.m[id] = s
	return true, nil
}
func (f *fakeSessions) Revoke(_ context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.m[id]
	if !ok {
		return ErrNotFound
	}
	if s.RevokedAt.IsZero() {
		s.RevokedAt = at
	}
	f.m[id] = s
	return nil
}

type fakeRegistrar struct {
	users       *fakeUsers
	sessions    *fakeSessions
	limitHit    bool
	lastPrimary bool
}

func (f *fakeRegistrar) RegisterDevice(_ context.Context, p RegisterDeviceParams) (RegisterDeviceResult, error) {
	if f.limitHit {
		return RegisterDeviceResult{}, ErrDeviceLimit
	}
	f.users.mu.Lock()
	u, exists := f.users.m[string(p.PhoneHash)]
	if !exists {
		u = User{ID: p.UserID}
		f.users.m[string(p.PhoneHash)] = u
	}
	f.users.mu.Unlock()
	f.lastPrimary = !exists
	f.sessions.add(domain.Session{
		ID: p.SessionID, DeviceID: p.DeviceID, UserID: u.ID,
		RefreshHash: p.RefreshHash, ExpiresAt: p.SessionExpiresAt,
	})
	return RegisterDeviceResult{UserID: u.ID, NewUser: !exists}, nil
}

type recordingSender struct {
	mu   sync.Mutex
	last map[string]string
}

func (r *recordingSender) Send(_ context.Context, dest, code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last[dest] = code
	return nil
}
func (r *recordingSender) Channel() domain.Channel { return domain.ChannelMock }
func (r *recordingSender) codeFor(dest string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last[dest]
}

type memLimiter struct{ m *ratelimit.MemoryLimiter }

func (l memLimiter) Allow(_ context.Context, key string, p ratelimit.Params) (ratelimit.Result, error) {
	return l.m.Allow(key, p)
}

type denyLimiter struct{}

func (denyLimiter) Allow(_ context.Context, _ string, _ ratelimit.Params) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: false, RetryAfter: 42 * time.Second}, nil
}

type nopAttempts struct{}

func (nopAttempts) Record(_ context.Context, _ []byte, _ bool, _ time.Time) error { return nil }

// ─────────────────────────────── harness ────────────────────────────────

type harness struct {
	svc      *Service
	sender   *recordingSender
	users    *fakeUsers
	sessions *fakeSessions
	reg      *fakeRegistrar
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	issuer, err := NewEphemeralIssuer(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	users := &fakeUsers{m: map[string]User{}}
	sessions := &fakeSessions{m: map[string]domain.Session{}}
	reg := &fakeRegistrar{users: users, sessions: sessions}
	sender := &recordingSender{last: map[string]string{}}
	svc := NewService(Deps{
		Challenges: &fakeChallenges{m: map[string]domain.Challenge{}},
		Users:      users,
		Sessions:   sessions,
		Registrar:  reg,
		Sender:     sender,
		Limiter:    memLimiter{ratelimit.NewMemoryLimiter()},
		Attempts:   nopAttempts{},
		Issuer:     issuer,
		Log:        slog.Default(),
	}, "test-pepper", 90*24*time.Hour)
	svc.entropy = rand.Reader
	return &harness{svc: svc, sender: sender, users: users, sessions: sessions, reg: reg}
}

var testDevice = DeviceInfo{Platform: "android", Name: "Pixel", IdentityKey: []byte("pubkey")}

func authCode(t *testing.T, e error) string {
	t.Helper()
	var ae *Error
	if !errors.As(e, &ae) {
		t.Fatalf("expected *auth.Error, got %T: %v", e, e)
	}
	return ae.Code
}

// ─────────────────────────────── tests ──────────────────────────────────

func TestFlow_NewUserRegistration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	ch, err := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550100", IP: "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
	code := h.sender.codeFor("+14155550100")
	if len(code) != 6 {
		t.Fatalf("sender did not receive a 6-digit code: %q", code)
	}

	pair, err := h.svc.VerifyOTP(ctx, ch.ChallengeID, code, testDevice)
	if err != nil {
		t.Fatal(err)
	}
	if pair.RequiresPIN || pair.AccessJWT == "" || pair.RefreshToken == "" {
		t.Fatalf("expected tokens, got %+v", pair)
	}
	ident, err := h.svc.Verify(pair.AccessJWT)
	if err != nil || ident.UserID != pair.UserID || ident.DeviceID != pair.DeviceID {
		t.Fatalf("issued jwt does not verify to the same identity: %+v err=%v", ident, err)
	}
	if !h.reg.lastPrimary {
		t.Fatal("first device must register as primary")
	}
}

func TestVerifyOTP_WrongCodeFiveTimes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ch, _ := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550101", IP: "1.1.1.1"})

	for i := 0; i < domain.OTPMaxAttempts; i++ {
		if _, err := h.svc.VerifyOTP(ctx, ch.ChallengeID, "000000", testDevice); authCode(t, err) != "AUTH_OTP_INVALID" {
			t.Fatalf("attempt %d: wrong error", i+1)
		}
	}
	// Even the CORRECT code is now refused: attempts exhausted.
	code := h.sender.codeFor("+14155550101")
	if _, err := h.svc.VerifyOTP(ctx, ch.ChallengeID, code, testDevice); authCode(t, err) != "AUTH_OTP_INVALID" {
		t.Fatal("correct code accepted after attempt exhaustion")
	}
}

func TestRequestOTP_RateLimited(t *testing.T) {
	h := newHarness(t)
	h.svc.d.Limiter = denyLimiter{}
	_, err := h.svc.RequestOTP(context.Background(), OTPRequest{Handle: "+14155550102", IP: "1.1.1.1"})
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != "RATE_LIMITED" || ae.RetryAfter != 42*time.Second {
		t.Fatalf("want RATE_LIMITED with retry-after, got %v", err)
	}
}

func TestRequestOTP_InvalidHandle(t *testing.T) {
	h := newHarness(t)
	for _, bad := range []string{"", "12345", "+0123", "not-a-phone", "+1 415 555"} {
		if _, err := h.svc.RequestOTP(context.Background(), OTPRequest{Handle: bad, IP: "1.1.1.1"}); authCode(t, err) != "VALIDATION_PHONE" {
			t.Fatalf("handle %q not rejected", bad)
		}
	}
}

func TestFlow_PINGate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// Register once, then set a PIN.
	ch, _ := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550103", IP: "1.1.1.1"})
	pair, err := h.svc.VerifyOTP(ctx, ch.ChallengeID, h.sender.codeFor("+14155550103"), testDevice)
	if err != nil {
		t.Fatal(err)
	}
	ident, _ := h.svc.Verify(pair.AccessJWT)
	if err := h.svc.SetPIN(ctx, ident, "", "482913"); err != nil {
		t.Fatal(err)
	}

	// Re-registration now demands the PIN.
	ch2, _ := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550103", IP: "1.1.1.1"})
	pair2, err := h.svc.VerifyOTP(ctx, ch2.ChallengeID, h.sender.codeFor("+14155550103"), testDevice)
	if err != nil || !pair2.RequiresPIN {
		t.Fatalf("want RequiresPIN, got %+v err=%v", pair2, err)
	}
	if _, err := h.svc.VerifyPIN(ctx, ch2.ChallengeID, "999999", testDevice); authCode(t, err) != "AUTH_PIN_INVALID" {
		t.Fatal("wrong pin accepted")
	}
	pair3, err := h.svc.VerifyPIN(ctx, ch2.ChallengeID, "482913", testDevice)
	if err != nil || pair3.AccessJWT == "" {
		t.Fatalf("correct pin rejected: %v", err)
	}
}

func TestRefresh_RotationAndReuse(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ch, _ := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550104", IP: "1.1.1.1"})
	pair, _ := h.svc.VerifyOTP(ctx, ch.ChallengeID, h.sender.codeFor("+14155550104"), testDevice)

	// Rotate once.
	pair2, err := h.svc.Refresh(ctx, pair.RefreshToken)
	if err != nil || pair2.RefreshToken == pair.RefreshToken {
		t.Fatalf("rotation failed: %v", err)
	}
	// Replay the OLD token → reuse detection kills the session.
	if _, err := h.svc.Refresh(ctx, pair.RefreshToken); authCode(t, err) != "AUTH_REFRESH_REUSED" {
		t.Fatalf("old token replay not detected: %v", err)
	}
	// The rotated (newest) token is dead too — whole session revoked.
	if _, err := h.svc.Refresh(ctx, pair2.RefreshToken); authCode(t, err) != "AUTH_DEVICE_REVOKED" {
		t.Fatalf("session survived reuse detection: %v", err)
	}
}

func TestRefresh_UnknownToken(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Refresh(context.Background(), "bogus-token"); authCode(t, err) != "AUTH_TOKEN_INVALID" {
		t.Fatal("unknown token not rejected")
	}
}

func TestLogout_RevokesSession(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ch, _ := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550105", IP: "1.1.1.1"})
	pair, _ := h.svc.VerifyOTP(ctx, ch.ChallengeID, h.sender.codeFor("+14155550105"), testDevice)
	ident, _ := h.svc.Verify(pair.AccessJWT)

	if err := h.svc.Logout(ctx, ident); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Refresh(ctx, pair.RefreshToken); authCode(t, err) != "AUTH_DEVICE_REVOKED" {
		t.Fatal("refresh worked after logout")
	}
}

func TestVerifyOTP_SuspendedAccount(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ph := h.svc.PhoneHash("+14155550106")
	h.users.m[string(ph)] = User{ID: "u-sus", Status: 1}

	ch, _ := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550106", IP: "1.1.1.1"})
	_, err := h.svc.VerifyOTP(ctx, ch.ChallengeID, h.sender.codeFor("+14155550106"), testDevice)
	if authCode(t, err) != "ACCOUNT_SUSPENDED" {
		t.Fatalf("suspended account not blocked: %v", err)
	}
}

func TestRegister_DeviceLimit(t *testing.T) {
	h := newHarness(t)
	h.reg.limitHit = true
	ctx := context.Background()
	ch, _ := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550107", IP: "1.1.1.1"})
	_, err := h.svc.VerifyOTP(ctx, ch.ChallengeID, h.sender.codeFor("+14155550107"), testDevice)
	if authCode(t, err) != "STATE_DEVICE_LIMIT" {
		t.Fatalf("device limit not mapped: %v", err)
	}
}

func TestRequestOTP_NoEnumeration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// One handle has an account, one doesn't — responses must be shaped identically.
	ph := h.svc.PhoneHash("+14155550108")
	h.users.m[string(ph)] = User{ID: "u-exists"}

	a, err1 := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550108", IP: "9.9.9.9"})
	b, err2 := h.svc.RequestOTP(ctx, OTPRequest{Handle: "+14155550199", IP: "9.9.9.8"})
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if a.Channel != b.Channel || a.ChallengeID == "" || b.ChallengeID == "" {
		t.Fatalf("responses distinguishable: %+v vs %+v", a, b)
	}
}
