// Package domain holds the device-auth pure logic (T10.02): WebAuthn passkey
// verification (challenge, clientDataJSON + authenticatorData parsing, ES256 /
// EdDSA assertion signature verification) and the suspicious-login heuristic. No
// I/O, no third-party crypto — Go stdlib only. WebAuthn is standard public-key
// signature verification (not the E2EE ratchet); libsignal is untouched.
package domain

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
)

// COSE algorithm identifiers we support (the platform authenticators' defaults).
const (
	AlgES256 = -7 // ECDSA P-256 + SHA-256
	AlgEdDSA = -8 // Ed25519
)

var (
	ErrBadClientData = errors.New("webauthn: malformed clientDataJSON")
	ErrClientType    = errors.New("webauthn: unexpected clientData.type")
	ErrChallenge     = errors.New("webauthn: challenge mismatch")
	ErrOrigin        = errors.New("webauthn: origin mismatch")
	ErrBadAuthData   = errors.New("webauthn: malformed authenticatorData")
	ErrUserPresence  = errors.New("webauthn: user-presence flag not set")
	ErrRPID          = errors.New("webauthn: rpIdHash mismatch")
	ErrSignature     = errors.New("webauthn: signature verification failed")
	ErrUnsupported   = errors.New("webauthn: unsupported COSE algorithm")
	ErrBadKey        = errors.New("webauthn: malformed public key")
	ErrSignCount     = errors.New("webauthn: sign count went backwards (possible cloned authenticator)")
)

// b64url is WebAuthn's raw-URL (no padding) base64.
var b64url = base64.RawURLEncoding

// GenChallenge returns a fresh 32-byte challenge, base64url-encoded (the form the
// browser echoes back in clientDataJSON).
func GenChallenge() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return b64url.EncodeToString(buf), nil
}

// ClientData is the parsed clientDataJSON.
type ClientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

// ParseClientData decodes clientDataJSON.
func ParseClientData(b []byte) (ClientData, error) {
	var cd ClientData
	if err := json.Unmarshal(b, &cd); err != nil {
		return ClientData{}, ErrBadClientData
	}
	return cd, nil
}

// VerifyClientData checks the ceremony type, challenge, and origin.
func VerifyClientData(cd ClientData, wantType, wantChallenge, wantOrigin string) error {
	if cd.Type != wantType {
		return ErrClientType
	}
	if !subtleEq(cd.Challenge, wantChallenge) {
		return ErrChallenge
	}
	if cd.Origin != wantOrigin {
		return ErrOrigin
	}
	return nil
}

// AuthData is the parsed authenticatorData prefix (rpIdHash + flags + signCount).
type AuthData struct {
	RPIDHash  [32]byte
	Flags     byte
	SignCount uint32
}

const flagUserPresent = 0x01

// ParseAuthData reads the fixed 37-byte authenticatorData prefix.
func ParseAuthData(b []byte) (AuthData, error) {
	if len(b) < 37 {
		return AuthData{}, ErrBadAuthData
	}
	var ad AuthData
	copy(ad.RPIDHash[:], b[:32])
	ad.Flags = b[32]
	ad.SignCount = binary.BigEndian.Uint32(b[33:37])
	return ad, nil
}

// CheckAuthData verifies the rpIdHash matches the RP id and user-presence is set.
func CheckAuthData(ad AuthData, rpID string) error {
	if ad.Flags&flagUserPresent == 0 {
		return ErrUserPresence
	}
	want := sha256.Sum256([]byte(rpID))
	if ad.RPIDHash != want {
		return ErrRPID
	}
	return nil
}

// Credential is a stored passkey public key.
type Credential struct {
	Alg int
	// For ES256: the 32-byte big-endian X and Y coordinates.
	X, Y []byte
	// For EdDSA: the 32-byte public key.
	Ed []byte
}

// VerifyAssertion verifies a WebAuthn assertion signature: the authenticator
// signs `authData || SHA256(clientDataJSON)` with the credential's private key.
func VerifyAssertion(cred Credential, authData, clientDataJSON, sig []byte) error {
	cdHash := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte{}, authData...), cdHash[:]...)

	switch cred.Alg {
	case AlgES256:
		pub, err := es256PublicKey(cred.X, cred.Y)
		if err != nil {
			return err
		}
		h := sha256.Sum256(signed)
		// WebAuthn ECDSA signatures are ASN.1 DER-encoded.
		if !ecdsa.VerifyASN1(pub, h[:], sig) {
			return ErrSignature
		}
		return nil
	case AlgEdDSA:
		if len(cred.Ed) != ed25519.PublicKeySize {
			return ErrBadKey
		}
		if !ed25519.Verify(ed25519.PublicKey(cred.Ed), signed, sig) {
			return ErrSignature
		}
		return nil
	default:
		return ErrUnsupported
	}
}

// NextSignCount validates the assertion's sign count against the stored one and
// returns the value to persist. Authenticators either count up or report 0/0.
func NextSignCount(stored, presented uint32) (uint32, error) {
	if stored == 0 && presented == 0 {
		return 0, nil // authenticator doesn't implement a counter
	}
	if presented <= stored {
		return stored, ErrSignCount
	}
	return presented, nil
}

func es256PublicKey(x, y []byte) (*ecdsa.PublicKey, error) {
	if len(x) != 32 || len(y) != 32 {
		return nil, ErrBadKey
	}
	// Validate the point is a valid P-256 public key (on-curve + range) via
	// crypto/ecdh, which supersedes the deprecated Curve.IsOnCurve. The uncompressed
	// SEC1 encoding is 0x04 || x || y.
	uncompressed := append([]byte{0x04}, append(append([]byte{}, x...), y...)...)
	if _, err := ecdh.P256().NewPublicKey(uncompressed); err != nil {
		return nil, ErrBadKey
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
}

// DecodeB64URL decodes a raw-URL base64 field (challenge / key coordinate / sig).
func DecodeB64URL(s string) ([]byte, error) { return b64url.DecodeString(s) }

// subtleEq compares two equal-length-ish strings without early-out on length —
// challenge comparison. (Not the DB path; a small constant-time courtesy.)
func subtleEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// ── suspicious-login heuristic ───────────────────────────────────────────────

// IsSuspiciousLogin flags a login whose IP the user hasn't been seen from before.
// A coarse, metadata-only signal (no geo lookup) — the client surfaces it so the
// user can spot an unexpected sign-in.
func IsSuspiciousLogin(knownIPs []string, ip string) bool {
	if ip == "" {
		return false
	}
	for _, k := range knownIPs {
		if k == ip {
			return false
		}
	}
	return true
}
