package deviceauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/deviceauth/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

var b64 = base64.RawURLEncoding

// ── fake store ───────────────────────────────────────────────────────────────

type fakeStore struct {
	challenges map[string]Challenge
	creds      map[string]Credential
	logins     []LoginEvent
}

func newFake() *fakeStore {
	return &fakeStore{challenges: map[string]Challenge{}, creds: map[string]Credential{}}
}
func (s *fakeStore) SaveChallenge(_ context.Context, c Challenge) error {
	s.challenges[c.Value] = c
	return nil
}
func (s *fakeStore) TakeChallenge(_ context.Context, value string) (Challenge, error) {
	c, ok := s.challenges[value]
	if !ok {
		return Challenge{}, ErrNotFound
	}
	delete(s.challenges, value)
	return c, nil
}
func (s *fakeStore) CreateCredential(_ context.Context, c Credential) error {
	s.creds[c.ID] = c
	return nil
}
func (s *fakeStore) GetCredential(_ context.Context, id string) (Credential, error) {
	c, ok := s.creds[id]
	if !ok {
		return Credential{}, ErrNotFound
	}
	return c, nil
}
func (s *fakeStore) ListCredentials(_ context.Context, userID string) ([]Credential, error) {
	var out []Credential
	for _, c := range s.creds {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}
func (s *fakeStore) UpdateSignCount(_ context.Context, id string, count uint32, usedAt time.Time) error {
	c := s.creds[id]
	c.SignCount = count
	c.LastUsedAt = &usedAt
	s.creds[id] = c
	return nil
}
func (s *fakeStore) DeleteCredential(_ context.Context, userID, id string) error {
	if c, ok := s.creds[id]; ok && c.UserID == userID {
		delete(s.creds, id)
	}
	return nil
}
func (s *fakeStore) RecordLogin(_ context.Context, e LoginEvent) error {
	s.logins = append(s.logins, e)
	return nil
}
func (s *fakeStore) KnownIPs(_ context.Context, userID string) ([]string, error) {
	var out []string
	for _, e := range s.logins {
		if e.UserID == userID {
			out = append(out, e.IP)
		}
	}
	return out, nil
}
func (s *fakeStore) RecentLogins(_ context.Context, userID string, limit int) ([]LoginEvent, error) {
	var out []LoginEvent
	for i := len(s.logins) - 1; i >= 0 && len(out) < limit; i-- {
		if s.logins[i].UserID == userID {
			out = append(out, s.logins[i])
		}
	}
	return out, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

func newSvc() (*Service, *fakeStore) {
	svc := NewService(newFake(), "wa.local", "https://wa.local")
	n := 0
	svc.newID = func() string { n++; return fmt.Sprintf("id%d", n) }
	svc.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return svc, svc.store.(*fakeStore)
}

func clientData(t *testing.T, typ, challenge string) []byte {
	t.Helper()
	b, _ := json.Marshal(domain.ClientData{Type: typ, Challenge: challenge, Origin: "https://wa.local"})
	return b
}

func authDataBytes() []byte {
	h := sha256.Sum256([]byte("wa.local"))
	return append(append(append([]byte{}, h[:]...), 0x01), 0, 0, 0, 0)
}

func pad32(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestPasskeyRegisterThenAssert(t *testing.T) {
	svc, _ := newSvc()
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	// register
	reg, err := svc.BeginRegistration(context.Background(), who("alice"))
	if err != nil {
		t.Fatal(err)
	}
	pub := append(pad32(priv.X.Bytes()), pad32(priv.Y.Bytes())...)
	err = svc.FinishRegistration(context.Background(), who("alice"), FinishRegistrationInput{
		CredentialID:   "cred-1",
		Alg:            domain.AlgES256,
		PublicKeyB64:   b64.EncodeToString(pub),
		ClientDataJSON: b64.EncodeToString(clientData(t, "webauthn.create", reg.Challenge)),
		Name:           "MacBook Touch ID",
	})
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}

	// login (assert)
	lo, err := svc.BeginLogin(context.Background(), who("alice"))
	if err != nil || len(lo.AllowedCredIDs) != 1 || lo.AllowedCredIDs[0] != "cred-1" {
		t.Fatalf("begin login: %v %+v", err, lo)
	}
	cdJSON := clientData(t, "webauthn.get", lo.Challenge)
	authData := authDataBytes()
	signed := append(append([]byte{}, authData...), sha256sum(cdJSON)...)
	digest := sha256.Sum256(signed)
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	err = svc.FinishLogin(context.Background(), who("alice"), FinishLoginInput{
		CredentialID:      "cred-1",
		AuthenticatorData: b64.EncodeToString(authData),
		ClientDataJSON:    b64.EncodeToString(cdJSON),
		Signature:         b64.EncodeToString(sig),
	})
	if err != nil {
		t.Fatalf("finish login should succeed: %v", err)
	}
}

func TestFinishRegistrationRejectsUnknownChallenge(t *testing.T) {
	svc, _ := newSvc()
	err := svc.FinishRegistration(context.Background(), who("alice"), FinishRegistrationInput{
		CredentialID:   "c",
		Alg:            domain.AlgES256,
		PublicKeyB64:   b64.EncodeToString(make([]byte, 64)),
		ClientDataJSON: b64.EncodeToString(clientData(t, "webauthn.create", "never-issued")),
	})
	if codeOf(t, err) != "AUTH_CHALLENGE" {
		t.Fatalf("want AUTH_CHALLENGE, got %v", err)
	}
}

func TestFinishLoginRejectsForgedAssertion(t *testing.T) {
	svc, store := newSvc()
	// enrol a credential directly
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub := append(pad32(priv.X.Bytes()), pad32(priv.Y.Bytes())...)
	store.creds["cred-1"] = Credential{ID: "cred-1", UserID: "alice", Alg: domain.AlgES256, PublicKey: pub}

	lo, _ := svc.BeginLogin(context.Background(), who("alice"))
	cdJSON := clientData(t, "webauthn.get", lo.Challenge)
	// sign with a DIFFERENT key → verification must fail
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	signed := append(append([]byte{}, authDataBytes()...), sha256sum(cdJSON)...)
	d := sha256.Sum256(signed)
	sig, _ := ecdsa.SignASN1(rand.Reader, other, d[:])
	err := svc.FinishLogin(context.Background(), who("alice"), FinishLoginInput{
		CredentialID: "cred-1", AuthenticatorData: b64.EncodeToString(authDataBytes()),
		ClientDataJSON: b64.EncodeToString(cdJSON), Signature: b64.EncodeToString(sig),
	})
	if codeOf(t, err) != "AUTH_PASSKEY" {
		t.Fatalf("forged assertion should fail with AUTH_PASSKEY, got %v", err)
	}
}

func TestLoginAuditFlagsNewIP(t *testing.T) {
	svc, _ := newSvc()
	svc.Observe(context.Background(), "alice", "dev-1", "1.2.3.4", "Safari")
	svc.Observe(context.Background(), "alice", "dev-1", "1.2.3.4", "Safari") // same IP
	svc.Observe(context.Background(), "alice", "dev-2", "9.9.9.9", "Chrome") // new IP

	logins, err := svc.RecentLogins(context.Background(), who("alice"))
	if err != nil || len(logins) != 3 {
		t.Fatalf("recent logins: %v %+v", err, logins)
	}
	// newest first: the new-IP login is suspicious, the repeat is not
	if !logins[0].Suspicious || logins[1].Suspicious {
		t.Fatalf("suspicious flags wrong: %+v", logins)
	}
}

func sha256sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
