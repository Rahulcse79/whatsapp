package calls

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

// LiveKit join tokens (rtc-lld §1): short-lived, room-scoped JWTs the client
// presents to the SFU. A LiveKit access token is a plain HS256 JWT signed with
// the API secret; the grants live under the `video` claim. Minted here (no SDK
// dependency) so the format is explicit and unit-testable.

// JoinGrant is what a token authorizes: an identity joining one room, with
// publish/subscribe capability.
type JoinGrant struct {
	Identity     string // opaque participant id (user:device)
	Room         string
	CanPublish   bool
	CanSubscribe bool
}

// TokenMinter signs LiveKit access tokens with the deployment's API key/secret.
type TokenMinter struct {
	apiKey    string
	apiSecret []byte
}

func NewTokenMinter(apiKey, apiSecret string) *TokenMinter {
	return &TokenMinter{apiKey: apiKey, apiSecret: []byte(apiSecret)}
}

type videoGrant struct {
	Room           string `json:"room"`
	RoomJoin       bool   `json:"roomJoin"`
	CanPublish     bool   `json:"canPublish"`
	CanSubscribe   bool   `json:"canSubscribe"`
	CanPublishData bool   `json:"canPublishData"`
}

type accessClaims struct {
	Iss   string     `json:"iss"` // API key
	Sub   string     `json:"sub"` // identity
	Name  string     `json:"name,omitempty"`
	Nbf   int64      `json:"nbf"`
	Exp   int64      `json:"exp"`
	Video videoGrant `json:"video"`
}

// Mint returns a signed LiveKit join token valid for ttl from now.
func (m *TokenMinter) Mint(g JoinGrant, ttl time.Duration, now time.Time) (string, error) {
	claims := accessClaims{
		Iss:  m.apiKey,
		Sub:  g.Identity,
		Name: g.Identity,
		Nbf:  now.Add(-5 * time.Second).Unix(), // small skew tolerance
		Exp:  now.Add(ttl).Unix(),
		Video: videoGrant{
			Room:           g.Room,
			RoomJoin:       true,
			CanPublish:     g.CanPublish,
			CanSubscribe:   g.CanSubscribe,
			CanPublishData: g.CanPublish,
		},
	}
	return m.sign(claims)
}

func (m *TokenMinter) sign(claims accessClaims) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, m.apiSecret)
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + sig, nil
}
