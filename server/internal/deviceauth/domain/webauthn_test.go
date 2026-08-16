package domain

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"testing"
)

const rpID = "wa.local"
const origin = "https://wa.local"

func pad32(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// buildAssertion returns (authData, clientDataJSON, signedDigestInput) for a
// "webauthn.get" ceremony over the given challenge.
func buildAssertion(t *testing.T, challenge string) (authData, clientDataJSON, signed []byte) {
	t.Helper()
	cd := ClientData{Type: "webauthn.get", Challenge: challenge, Origin: origin}
	clientDataJSON, _ = json.Marshal(cd)
	rpHash := sha256.Sum256([]byte(rpID))
	authData = append(authData, rpHash[:]...)
	authData = append(authData, flagUserPresent) // flags
	authData = append(authData, 0, 0, 0, 0)      // signCount = 0
	cdHash := sha256.Sum256(clientDataJSON)
	signed = append(append([]byte{}, authData...), cdHash[:]...)
	return
}

func TestVerifyAssertion_ES256(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ch, _ := GenChallenge()
	authData, clientDataJSON, signed := buildAssertion(t, ch)
	h := sha256.Sum256(signed)
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, h[:])

	cred := Credential{Alg: AlgES256, X: pad32(priv.X.Bytes()), Y: pad32(priv.Y.Bytes())}
	if err := VerifyAssertion(cred, authData, clientDataJSON, sig); err != nil {
		t.Fatalf("valid ES256 assertion rejected: %v", err)
	}
	// tamper with the signed clientData → signature no longer matches
	bad := append([]byte{}, clientDataJSON...)
	bad[10] ^= 0xff
	if err := VerifyAssertion(cred, authData, bad, sig); err == nil {
		t.Fatal("tampered clientData should fail")
	}
}

func TestVerifyAssertion_EdDSA(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ch, _ := GenChallenge()
	authData, clientDataJSON, signed := buildAssertion(t, ch)
	sig := ed25519.Sign(priv, signed)

	cred := Credential{Alg: AlgEdDSA, Ed: pub}
	if err := VerifyAssertion(cred, authData, clientDataJSON, sig); err != nil {
		t.Fatalf("valid EdDSA assertion rejected: %v", err)
	}
	sig[0] ^= 0x01
	if err := VerifyAssertion(cred, authData, clientDataJSON, sig); err == nil {
		t.Fatal("tampered signature should fail")
	}
}

func TestVerifyClientData(t *testing.T) {
	ch, _ := GenChallenge()
	cd := ClientData{Type: "webauthn.get", Challenge: ch, Origin: origin}
	if err := VerifyClientData(cd, "webauthn.get", ch, origin); err != nil {
		t.Fatalf("valid clientData rejected: %v", err)
	}
	if err := VerifyClientData(cd, "webauthn.create", ch, origin); err != ErrClientType {
		t.Fatal("wrong type should fail")
	}
	if err := VerifyClientData(cd, "webauthn.get", "other", origin); err != ErrChallenge {
		t.Fatal("wrong challenge should fail")
	}
	if err := VerifyClientData(cd, "webauthn.get", ch, "https://evil.example"); err != ErrOrigin {
		t.Fatal("wrong origin should fail")
	}
}

func TestCheckAuthData(t *testing.T) {
	ch, _ := GenChallenge()
	authData, _, _ := buildAssertion(t, ch)
	ad, err := ParseAuthData(authData)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckAuthData(ad, rpID); err != nil {
		t.Fatalf("valid authData rejected: %v", err)
	}
	if err := CheckAuthData(ad, "other.rp"); err != ErrRPID {
		t.Fatal("wrong rpId should fail")
	}
	noUP := ad
	noUP.Flags = 0
	if err := CheckAuthData(noUP, rpID); err != ErrUserPresence {
		t.Fatal("missing user-presence should fail")
	}
	if _, err := ParseAuthData([]byte{1, 2, 3}); err != ErrBadAuthData {
		t.Fatal("short authData should fail")
	}
}

func TestNextSignCount(t *testing.T) {
	if n, err := NextSignCount(0, 0); err != nil || n != 0 {
		t.Fatal("0/0 counter ok")
	}
	if n, err := NextSignCount(5, 6); err != nil || n != 6 {
		t.Fatal("forward counter ok")
	}
	if _, err := NextSignCount(6, 5); err != ErrSignCount {
		t.Fatal("backward counter should fail (cloned authenticator)")
	}
}

func TestIsSuspiciousLogin(t *testing.T) {
	known := []string{"1.2.3.4", "5.6.7.8"}
	if IsSuspiciousLogin(known, "1.2.3.4") {
		t.Fatal("known IP is not suspicious")
	}
	if !IsSuspiciousLogin(known, "9.9.9.9") {
		t.Fatal("new IP is suspicious")
	}
	if IsSuspiciousLogin(known, "") {
		t.Fatal("empty IP is not flagged")
	}
}
