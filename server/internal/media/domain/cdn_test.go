package domain

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

var cdnSecret = []byte("edge-signing-secret")

func TestSignVerifyRoundTrip(t *testing.T) {
	const key, exp, now = "media/2026/abc.bin", int64(2_000), int64(1_000)
	tok := SignCDNToken(cdnSecret, key, exp)
	if tok == "" {
		t.Fatal("empty token")
	}
	if err := VerifyCDNToken(cdnSecret, key, exp, now, tok); err != nil {
		t.Fatalf("valid token should verify: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	const key, exp = "media/abc.bin", int64(2_000)
	tok := SignCDNToken(cdnSecret, key, exp)

	cases := []struct {
		name  string
		check func() error
		want  error
	}{
		{"expired", func() error { return VerifyCDNToken(cdnSecret, key, exp, exp, tok) }, ErrCDNExpired},
		{"wrong secret", func() error { return VerifyCDNToken([]byte("other"), key, exp, 1, tok) }, ErrCDNBadSig},
		{"wrong key", func() error { return VerifyCDNToken(cdnSecret, "media/other.bin", exp, 1, tok) }, ErrCDNBadSig},
		{"expiry extended", func() error { return VerifyCDNToken(cdnSecret, key, exp+3600, 1, tok) }, ErrCDNBadSig},
		{"empty token", func() error { return VerifyCDNToken(cdnSecret, key, exp, 1, "") }, ErrCDNMalformed},
		{"garbage token", func() error { return VerifyCDNToken(cdnSecret, key, exp, 1, "not-a-signature") }, ErrCDNBadSig},
	}
	for _, c := range cases {
		if got := c.check(); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// A client must not be able to buy itself more time by editing the expiry: the
// expiry is inside the signed string, so bumping it invalidates the token.
func TestExpiryIsSigned(t *testing.T) {
	const key = "media/abc.bin"
	short := SignCDNToken(cdnSecret, key, 1_000)
	long := SignCDNToken(cdnSecret, key, 9_999)
	if short == long {
		t.Fatal("tokens for different expiries must differ")
	}
	if err := VerifyCDNToken(cdnSecret, key, 9_999, 1, short); err != ErrCDNBadSig {
		t.Fatalf("replaying a short token against a longer expiry must fail, got %v", err)
	}
}

// The signed string is newline-delimited so a key cannot absorb the boundary and
// collide with a different (key, expiry) pair.
func TestDelimiterCannotBeShifted(t *testing.T) {
	a := SignCDNToken(cdnSecret, "abc", 12)
	b := SignCDNToken(cdnSecret, "abc\n1", 2)
	if a == b {
		t.Fatal("key/expiry boundary is ambiguous — tokens collided")
	}
}

func TestBuildCDNURL(t *testing.T) {
	const key, exp = "media/2026/08/blob.bin", int64(1_700_000_000)
	raw, err := BuildCDNURL("https://cdn.example.test/media", key, cdnSecret, exp)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "cdn.example.test" {
		t.Fatalf("unexpected origin: %s", raw)
	}
	if !strings.HasSuffix(u.Path, "/media/"+key) {
		t.Fatalf("path should carry the base prefix and the key: %s", u.Path)
	}
	if got := u.Query().Get(ExpiryParam); got != strconv.FormatInt(exp, 10) {
		t.Fatalf("expiry param: %q", got)
	}
	// The URL's own signature must verify for the key it addresses.
	if err := VerifyCDNToken(cdnSecret, key, exp, exp-1, u.Query().Get(SigParam)); err != nil {
		t.Fatalf("URL signature must verify: %v", err)
	}
}

func TestBuildCDNURLTrailingSlashAndBadBase(t *testing.T) {
	withSlash, err := BuildCDNURL("https://cdn.example.test/media/", "k.bin", cdnSecret, 10)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withSlash, "//k.bin") {
		t.Fatalf("trailing slash should not double up: %s", withSlash)
	}
	for _, bad := range []string{"", "   ", "not-a-url", "/relative/only"} {
		if _, err := BuildCDNURL(bad, "k.bin", cdnSecret, 10); err != ErrCDNMalformed {
			t.Errorf("base %q should be rejected, got %v", bad, err)
		}
	}
}

// TestKnownVector pins the exact bytes the signer produces. The edge validates
// these tokens in a different language (deploy/compose/config/cdn/verify.js, and
// whatever a managed CDN's token-auth uses), and a silent drift on either side
// would 403 every media fetch. Verified equal to the njs/HMAC construction
// HMAC-SHA256(secret, key + "\n" + expiry) base64url-unpadded.
// This vector is NOT a credential: it is the deterministic output of
// HMAC-SHA256 over the fixed inputs below, under a throwaway test key that
// exists only in this file. It is checked in on purpose — recomputing it in the
// test would assert nothing. The gitleaks annotation says so explicitly, since a
// high-entropy base64 literal is otherwise indistinguishable from a real key.
func TestKnownVector(t *testing.T) {
	const (
		key    = "media/2026/08/blob.bin"
		exp    = int64(1_700_000_000)
		testKD = "edge-signing-secret" // throwaway key derivation input, not a secret
	)
	// gitleaks:allow — expected HMAC output for the fixed inputs above.
	const wantDigest = "ugG7yTvA2gryHvSkWpxZN9LD8RIJInVjtYSiNGtw-vc"

	if got := SignCDNToken([]byte(testKD), key, exp); got != wantDigest {
		t.Fatalf("signing changed — every edge implementation must be updated in lockstep\n got: %s\nwant: %s", got, wantDigest)
	}
}
