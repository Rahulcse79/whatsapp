package calls

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

const whKey, whSecret = "APIwh", "webhook-secret"

// signWebhook builds a LiveKit-style webhook auth token: an HS256 JWT (signed
// with the API secret) whose `sha256` claim is base64(SHA-256(body)).
func signWebhook(t *testing.T, key, secret string, body []byte, exp int64) string {
	t.Helper()
	sum := sha256.Sum256(body)
	claims := map[string]any{"iss": key, "exp": exp, "sha256": base64.StdEncoding.EncodeToString(sum[:])}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	cb, _ := json.Marshal(claims)
	signing := header + "." + base64.RawURLEncoding.EncodeToString(cb)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestWebhookVerify_Valid(t *testing.T) {
	v := NewWebhookVerifier(whKey, whSecret)
	body := []byte(`{"event":"room_finished","room":{"sid":"RM_1","name":"call-abc"},"id":"ev1"}`)
	auth := signWebhook(t, whKey, whSecret, body, time.Now().Add(time.Minute).Unix())

	ev, err := v.Verify("Bearer "+auth, body)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Event != "room_finished" || ev.Room.Name != "call-abc" || ev.ID != "ev1" {
		t.Fatalf("decoded event wrong: %+v", ev)
	}
}

func TestWebhookVerify_RejectsForgedSignature(t *testing.T) {
	v := NewWebhookVerifier(whKey, whSecret)
	body := []byte(`{"event":"room_started","room":{"name":"call-x"}}`)
	forged := signWebhook(t, whKey, "WRONG-secret", body, time.Now().Add(time.Minute).Unix())
	if _, err := v.Verify("Bearer "+forged, body); err == nil {
		t.Fatal("a signature under the wrong secret must be rejected")
	}
}

func TestWebhookVerify_RejectsTamperedBody(t *testing.T) {
	v := NewWebhookVerifier(whKey, whSecret)
	body := []byte(`{"event":"room_finished","room":{"name":"call-x"}}`)
	auth := signWebhook(t, whKey, whSecret, body, time.Now().Add(time.Minute).Unix())
	// Same (valid) token, but a swapped body → hash mismatch.
	if _, err := v.Verify("Bearer "+auth, []byte(`{"event":"room_finished","room":{"name":"call-EVIL"}}`)); err != ErrWebhookBody {
		t.Fatalf("tampered body → %v, want ErrWebhookBody", err)
	}
}

func TestWebhookVerify_RejectsWrongIssuerAndExpiry(t *testing.T) {
	v := NewWebhookVerifier(whKey, whSecret)
	body := []byte(`{"event":"room_started"}`)

	wrongIss := signWebhook(t, "OTHER", whSecret, body, time.Now().Add(time.Minute).Unix())
	if _, err := v.Verify("Bearer "+wrongIss, body); err != ErrWebhookAuth {
		t.Fatalf("wrong issuer → %v, want ErrWebhookAuth", err)
	}

	expired := signWebhook(t, whKey, whSecret, body, time.Now().Add(-time.Minute).Unix())
	if _, err := v.Verify("Bearer "+expired, body); err != ErrWebhookAuth {
		t.Fatalf("expired → %v, want ErrWebhookAuth", err)
	}
}
