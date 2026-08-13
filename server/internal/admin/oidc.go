package admin

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// OIDC ID-token verification is the SSO gate of HLD §15.6: admins authenticate
// against the deployment's IdP, which owns admin membership and role assignment.
// We verify the ID token's RS256 signature against the provider's JWKS, its
// issuer/audience/expiry, then read the RBAC role from a configured claim.

const defaultRoleClaim = "wa_admin_role"

// jwk is one key from a JWKS document (RFC 7517); only RSA signing keys are used.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"` // modulus, base64url
	E   string `json:"e"` // exponent, base64url
}

type jwksDoc struct {
	Keys []jwk `json:"keys"`
}

// KeySet resolves the IdP's signing keys by kid. Build it from the JWKS bytes
// ops fetch from the provider's jwks_uri and rebuild on rotation. It takes bytes
// (not a URL) on purpose — the HTTP fetch + cache is the ops seam around it, and
// keeping the parse pure makes verification testable without network.
type KeySet struct {
	keys map[string]*rsa.PublicKey
}

// NewKeySet parses a JWKS JSON document into a KeySet.
func NewKeySet(jwksJSON []byte) (*KeySet, error) {
	var doc jwksDoc
	if err := json.Unmarshal(jwksJSON, &doc); err != nil {
		return nil, fmt.Errorf("admin: parse jwks: %w", err)
	}
	ks := &KeySet{keys: make(map[string]*rsa.PublicKey, len(doc.Keys))}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		pub, err := k.toRSA()
		if err != nil {
			return nil, err
		}
		ks.keys[k.Kid] = pub
	}
	if len(ks.keys) == 0 {
		return nil, errors.New("admin: jwks has no usable RSA signing keys")
	}
	return ks, nil
}

// NewKeySetFromRSA builds a KeySet from already-parsed keys.
func NewKeySetFromRSA(keys map[string]*rsa.PublicKey) *KeySet {
	m := make(map[string]*rsa.PublicKey, len(keys))
	for k, v := range keys {
		m[k] = v
	}
	return &KeySet{keys: m}
}

func (k jwk) toRSA() (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("admin: jwk modulus: %w", err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("admin: jwk exponent: %w", err)
	}
	e := 0
	for _, b := range eb {
		e = e<<8 | int(b)
	}
	if e == 0 {
		return nil, errors.New("admin: jwk has a zero exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}, nil
}

func (ks *KeySet) key(kid string) (*rsa.PublicKey, bool) {
	k, ok := ks.keys[kid]
	return k, ok
}

// OIDCVerifier implements Verifier over an OIDC provider's ID tokens.
type OIDCVerifier struct {
	issuer    string
	audience  string
	roleClaim string
	keys      *KeySet
	now       func() time.Time
}

// NewOIDCVerifier builds a verifier. roleClaim defaults to "wa_admin_role" when
// empty; issuer and audience must match the tokens the IdP mints for this SPA.
func NewOIDCVerifier(issuer, audience, roleClaim string, keys *KeySet) *OIDCVerifier {
	if roleClaim == "" {
		roleClaim = defaultRoleClaim
	}
	return &OIDCVerifier{issuer: issuer, audience: audience, roleClaim: roleClaim, keys: keys, now: time.Now}
}

// Verify checks the RS256 signature, issuer, audience, and expiry, then extracts
// the subject, email, and role claim. Any failure is an authentication failure.
func (v *OIDCVerifier) Verify(_ context.Context, token string) (Claims, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.now),
	).ParseWithClaims(token, claims, v.keyfunc)
	if err != nil {
		return Claims{}, fmt.Errorf("admin: verify id token: %w", err)
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return Claims{}, errors.New("admin: id token has no subject")
	}
	email, _ := claims["email"].(string)
	role, _ := claims[v.roleClaim].(string)
	return Claims{Subject: sub, Email: email, Role: role}, nil
}

// keyfunc resolves the token's kid to a signing key. The RS256 method is already
// pinned by WithValidMethods, so algorithm confusion (alg=none / HS256 with the
// public key as secret) is rejected before this runs.
func (v *OIDCVerifier) keyfunc(t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("admin: id token has no kid")
	}
	key, ok := v.keys.key(kid)
	if !ok {
		return nil, fmt.Errorf("admin: unknown signing key %q", kid)
	}
	return key, nil
}
