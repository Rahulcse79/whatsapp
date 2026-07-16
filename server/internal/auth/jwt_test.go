package auth

import (
	"strings"
	"testing"
	"time"
)

func testIssuer(t *testing.T) *TokenIssuer {
	t.Helper()
	i, err := NewEphemeralIssuer(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestJWT_RoundTrip(t *testing.T) {
	i := testIssuer(t)
	tok, err := i.Issue("user-1", "dev-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := i.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	want := Identity{UserID: "user-1", DeviceID: "dev-1", SessionID: "sess-1"}
	if got != want {
		t.Fatalf("identity = %+v, want %+v", got, want)
	}
}

func TestJWT_ExpiryEnforced(t *testing.T) {
	i := testIssuer(t)
	tok, _ := i.Issue("u", "d", "s")
	i.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	if _, err := i.Verify(tok); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestJWT_WrongKeyRejected(t *testing.T) {
	a := testIssuer(t)
	b := testIssuer(t)
	tok, _ := a.Issue("u", "d", "s")
	if _, err := b.Verify(tok); err == nil {
		t.Fatal("token from another issuer's key accepted")
	}
}

func TestJWT_AlgNoneRejected(t *testing.T) {
	i := testIssuer(t)
	// alg=none forgery: header {"alg":"none","typ":"JWT"} + our claims, no sig.
	forged := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJpc3MiOiJ3aGF0c2FwcC12MiIsInN1YiI6InUiLCJkZXYiOiJkIn0."
	if _, err := i.Verify(forged); err == nil {
		t.Fatal("alg=none token accepted")
	}
}

func TestJWT_TamperedPayloadRejected(t *testing.T) {
	i := testIssuer(t)
	tok, _ := i.Issue("u", "d", "s")
	parts := strings.Split(tok, ".")
	parts[1] = parts[1][:len(parts[1])-2] + "xx"
	if _, err := i.Verify(strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered payload accepted")
	}
}

func TestJWT_SeedIssuer_Deterministic(t *testing.T) {
	seed := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 32 zero bytes, raw b64
	a, err := NewIssuerFromSeed(seed, "k1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewIssuerFromSeed(seed, "k1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := a.Issue("u", "d", "s")
	if _, err := b.Verify(tok); err != nil {
		t.Fatalf("same seed must verify same tokens: %v", err)
	}
}

func TestJWT_BadSeedRejected(t *testing.T) {
	if _, err := NewIssuerFromSeed("dG9vc2hvcnQ", "k", time.Minute); err == nil {
		t.Fatal("short seed accepted")
	}
	if _, err := NewIssuerFromSeed("!!!", "k", time.Minute); err == nil {
		t.Fatal("non-base64 seed accepted")
	}
}
