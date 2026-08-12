package calls

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// LiveKit webhook verification (rtc-lld §6). LiveKit signs each webhook POST with
// a JWT in the Authorization header whose `sha256` claim is the base64 SHA-256 of
// the raw body; the JWT itself is HS256-signed with the API secret. Verifying
// both the signature AND the body hash makes the handler safe against forgery and
// replay-with-tampered-body. Handlers are idempotent (LiveKit redelivers).

var (
	ErrWebhookAuth = errors.New("calls: webhook signature invalid")
	ErrWebhookBody = errors.New("calls: webhook body hash mismatch")
)

// WebhookEvent is the decoded LiveKit webhook payload (subset we act on).
type WebhookEvent struct {
	Event       string          `json:"event"` // room_started|room_finished|participant_joined|participant_left|track_published
	Room        WebhookRoom     `json:"room"`
	Participant WebhookIdentity `json:"participant"`
	ID          string          `json:"id"` // event id — idempotency key
}

type WebhookRoom struct {
	SID  string `json:"sid"`
	Name string `json:"name"` // our room_id
}

type WebhookIdentity struct {
	Identity string `json:"identity"`
}

// WebhookVerifier authenticates and decodes LiveKit webhooks.
type WebhookVerifier struct {
	apiKey    string
	apiSecret []byte
	now       func() time.Time
}

func NewWebhookVerifier(apiKey, apiSecret string) *WebhookVerifier {
	return &WebhookVerifier{apiKey: apiKey, apiSecret: []byte(apiSecret), now: time.Now}
}

// Verify checks the Authorization JWT against the body and returns the decoded
// event. It fails closed on any signature, hash, issuer, or expiry problem.
func (v *WebhookVerifier) Verify(authHeader string, body []byte) (WebhookEvent, error) {
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	claims, err := v.verifyJWT(token)
	if err != nil {
		return WebhookEvent{}, err
	}
	sum := sha256.Sum256(body)
	want := base64.StdEncoding.EncodeToString(sum[:])
	if !hmac.Equal([]byte(claims.SHA256), []byte(want)) {
		return WebhookEvent{}, ErrWebhookBody
	}
	var ev WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return WebhookEvent{}, err
	}
	return ev, nil
}

type webhookClaims struct {
	Iss    string `json:"iss"`
	Exp    int64  `json:"exp"`
	SHA256 string `json:"sha256"`
}

func (v *WebhookVerifier) verifyJWT(token string) (webhookClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return webhookClaims{}, ErrWebhookAuth
	}
	mac := hmac.New(sha256.New, v.apiSecret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return webhookClaims{}, ErrWebhookAuth
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return webhookClaims{}, ErrWebhookAuth
	}
	var c webhookClaims
	if err := json.Unmarshal(raw, &c); err != nil {
		return webhookClaims{}, ErrWebhookAuth
	}
	if c.Iss != v.apiKey {
		return webhookClaims{}, ErrWebhookAuth
	}
	if c.Exp != 0 && v.now().After(time.Unix(c.Exp, 0)) {
		return webhookClaims{}, ErrWebhookAuth
	}
	return c, nil
}
