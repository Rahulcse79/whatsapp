package calls

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTokenMinter_ProducesValidLiveKitJWT(t *testing.T) {
	const key, secret = "APIabc", "s3cr3t-livekit"
	m := NewTokenMinter(key, secret)
	now := time.Unix(1_800_000_000, 0)

	tok, err := m.Mint(JoinGrant{Identity: "u1:d1", Room: "call-xyz", CanPublish: true, CanSubscribe: true}, 60*time.Second, now)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	// Header.
	header := decodeSeg(t, parts[0])
	if header["alg"] != "HS256" || header["typ"] != "JWT" {
		t.Fatalf("bad header: %v", header)
	}

	// Signature must verify under the API secret.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != wantSig {
		t.Fatal("signature does not verify under the API secret")
	}

	// Claims: issuer, subject, TTL, and room-scoped video grant.
	claims := decodeSeg(t, parts[1])
	if claims["iss"] != key {
		t.Fatalf("iss = %v, want %s", claims["iss"], key)
	}
	if claims["sub"] != "u1:d1" {
		t.Fatalf("sub = %v", claims["sub"])
	}
	if exp := int64(claims["exp"].(float64)); exp != now.Add(60*time.Second).Unix() {
		t.Fatalf("exp = %d, want %d", exp, now.Add(60*time.Second).Unix())
	}
	video, ok := claims["video"].(map[string]any)
	if !ok {
		t.Fatalf("missing video grant: %v", claims)
	}
	if video["room"] != "call-xyz" || video["roomJoin"] != true || video["canPublish"] != true || video["canSubscribe"] != true {
		t.Fatalf("bad video grant: %v", video)
	}
}

func TestTokenMinter_SubscribeOnlyGrant(t *testing.T) {
	m := NewTokenMinter("k", "s")
	tok, err := m.Mint(JoinGrant{Identity: "u2:d2", Room: "r", CanPublish: false, CanSubscribe: true}, time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	claims := decodeSeg(t, strings.Split(tok, ".")[1])
	video := claims["video"].(map[string]any)
	if video["canPublish"] != false || video["canSubscribe"] != true {
		t.Fatalf("subscribe-only grant wrong: %v", video)
	}
}

func decodeSeg(t *testing.T, seg string) map[string]any {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		t.Fatalf("decode segment: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal segment: %v", err)
	}
	return m
}
