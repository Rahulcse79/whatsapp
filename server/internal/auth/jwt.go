package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const jwtIssuer = "whatsapp-v2"

// Claims carried by access JWTs (10-minute lifetime, HLD §15.2).
type Claims struct {
	jwt.RegisteredClaims
	DeviceID  string `json:"dev"`
	SessionID string `json:"sid"`
}

// TokenIssuer signs and verifies EdDSA access tokens. Old kids keep
// verifying after rotation; only the newest signs.
type TokenIssuer struct {
	kid  string
	priv ed25519.PrivateKey
	pubs map[string]ed25519.PublicKey
	ttl  time.Duration
	now  func() time.Time
}

// NewIssuerFromSeed builds an issuer from a base64-encoded 32-byte seed
// (WA_JWT_ED25519_SEED — mandatory in prod, config guard).
func NewIssuerFromSeed(seedB64, kid string, ttl time.Duration) (*TokenIssuer, error) {
	seed, err := base64.RawStdEncoding.DecodeString(seedB64)
	if err != nil {
		seed, err = base64.StdEncoding.DecodeString(seedB64) // tolerate padded input
	}
	if err != nil {
		return nil, fmt.Errorf("auth: jwt seed is not base64: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("auth: jwt seed must decode to %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	return newIssuer(ed25519.NewKeyFromSeed(seed), kid, ttl), nil
}

// NewEphemeralIssuer generates a throwaway key — dev/tests only: every
// restart invalidates all outstanding tokens.
func NewEphemeralIssuer(ttl time.Duration) (*TokenIssuer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("auth: generating ephemeral jwt key: %w", err)
	}
	return newIssuer(priv, "ephemeral", ttl), nil
}

func newIssuer(priv ed25519.PrivateKey, kid string, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{
		kid:  kid,
		priv: priv,
		pubs: map[string]ed25519.PublicKey{kid: priv.Public().(ed25519.PublicKey)},
		ttl:  ttl,
		now:  time.Now,
	}
}

// Issue mints an access token.
func (i *TokenIssuer) Issue(userID, deviceID, sessionID string) (string, error) {
	now := i.now()
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
		},
		DeviceID:  deviceID,
		SessionID: sessionID,
	})
	t.Header["kid"] = i.kid
	signed, err := t.SignedString(i.priv)
	if err != nil {
		return "", fmt.Errorf("auth: signing token: %w", err)
	}
	return signed, nil
}

// Verify parses and validates a token. Only EdDSA is accepted — the
// WithValidMethods pin is the alg-confusion guard.
func (i *TokenIssuer) Verify(token string) (Identity, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(t *jwt.Token) (any, error) {
			kid, _ := t.Header["kid"].(string)
			pub, ok := i.pubs[kid]
			if !ok {
				return nil, fmt.Errorf("unknown kid %q", kid)
			}
			return pub, nil
		},
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return i.now() }),
	)
	if err != nil {
		return Identity{}, fmt.Errorf("auth: token invalid: %w", err)
	}
	if claims.Subject == "" || claims.DeviceID == "" {
		return Identity{}, errors.New("auth: token missing identity claims")
	}
	return Identity{UserID: claims.Subject, DeviceID: claims.DeviceID, SessionID: claims.SessionID}, nil
}
