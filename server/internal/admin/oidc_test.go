package admin

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer = "https://idp.example/"
	testAud    = "wa-admin-spa"
	testKid    = "k1"
)

func genKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func baseClaims() jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":           testIssuer,
		"aud":           testAud,
		"sub":           "oidc|admin-1",
		"email":         "root@example",
		"wa_admin_role": "owner",
		"iat":           now.Unix(),
		"exp":           now.Add(time.Hour).Unix(),
	}
}

func mintRS256(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newVerifier(priv *rsa.PrivateKey) *OIDCVerifier {
	ks := NewKeySetFromRSA(map[string]*rsa.PublicKey{testKid: &priv.PublicKey})
	return NewOIDCVerifier(testIssuer, testAud, "", ks) // default role claim
}

func TestOIDCVerify_HappyPath(t *testing.T) {
	priv := genKey(t)
	claims, err := newVerifier(priv).Verify(context.Background(), mintRS256(t, priv, testKid, baseClaims()))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "oidc|admin-1" || claims.Email != "root@example" || claims.Role != "owner" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestOIDCVerify_Rejections(t *testing.T) {
	priv := genKey(t)
	other := genKey(t)
	v := newVerifier(priv)
	ctx := context.Background()

	t.Run("expired", func(t *testing.T) {
		c := baseClaims()
		c["exp"] = time.Now().Add(-time.Minute).Unix()
		if _, err := v.Verify(ctx, mintRS256(t, priv, testKid, c)); err == nil {
			t.Fatal("expired token accepted")
		}
	})
	t.Run("wrong issuer", func(t *testing.T) {
		c := baseClaims()
		c["iss"] = "https://evil.example/"
		if _, err := v.Verify(ctx, mintRS256(t, priv, testKid, c)); err == nil {
			t.Fatal("wrong issuer accepted")
		}
	})
	t.Run("wrong audience", func(t *testing.T) {
		c := baseClaims()
		c["aud"] = "some-other-app"
		if _, err := v.Verify(ctx, mintRS256(t, priv, testKid, c)); err == nil {
			t.Fatal("wrong audience accepted")
		}
	})
	t.Run("signature by an unknown key", func(t *testing.T) {
		// Signed by `other` but presented with our known kid → sig check fails.
		if _, err := v.Verify(ctx, mintRS256(t, other, testKid, baseClaims())); err == nil {
			t.Fatal("token signed by an unknown key accepted")
		}
	})
	t.Run("unknown kid", func(t *testing.T) {
		if _, err := v.Verify(ctx, mintRS256(t, priv, "rotated-out", baseClaims())); err == nil {
			t.Fatal("token with an unknown kid accepted")
		}
	})
	t.Run("missing kid", func(t *testing.T) {
		if _, err := v.Verify(ctx, mintRS256(t, priv, "", baseClaims())); err == nil {
			t.Fatal("token without a kid accepted")
		}
	})
	t.Run("no expiry", func(t *testing.T) {
		c := baseClaims()
		delete(c, "exp")
		if _, err := v.Verify(ctx, mintRS256(t, priv, testKid, c)); err == nil {
			t.Fatal("token without exp accepted")
		}
	})
	t.Run("alg confusion (HS256)", func(t *testing.T) {
		// A forged HS256 token must be rejected before the keyfunc runs
		// (WithValidMethods pins RS256) — no algorithm-confusion downgrade.
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, baseClaims())
		tok.Header["kid"] = testKid
		hs, err := tok.SignedString([]byte("public-key-as-secret"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.Verify(ctx, hs); err == nil {
			t.Fatal("HS256 token accepted against an RS256 verifier")
		}
	})
	t.Run("tampered payload", func(t *testing.T) {
		tok := mintRS256(t, priv, testKid, baseClaims())
		// Flip a character in the payload segment; signature no longer matches.
		b := []byte(tok)
		mid := len(b) / 2
		if b[mid] == 'a' {
			b[mid] = 'b'
		} else {
			b[mid] = 'a'
		}
		if _, err := v.Verify(ctx, string(b)); err == nil {
			t.Fatal("tampered token accepted")
		}
	})
}

// TestKeySetFromJWKS exercises the real JWKS-parsing path ops use: a provider
// JWKS document (n/e base64url) → KeySet → a successful verification.
func TestKeySetFromJWKS(t *testing.T) {
	priv := genKey(t)
	pub := &priv.PublicKey
	doc := map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "kid": testKid, "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := NewKeySet(raw)
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}
	v := NewOIDCVerifier(testIssuer, testAud, "", ks)
	if _, err := v.Verify(context.Background(), mintRS256(t, priv, testKid, baseClaims())); err != nil {
		t.Fatalf("verify with JWKS-parsed key: %v", err)
	}

	if _, err := NewKeySet([]byte(`{"keys":[]}`)); err == nil {
		t.Fatal("empty JWKS should be rejected")
	}
}
