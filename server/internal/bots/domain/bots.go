// Package domain holds the bot-framework pure logic (T13.02): handle/name/webhook
// validation, HMAC-SHA256 webhook signing/verification (so a bot can trust an
// event really came from us), and interactive-message validation. No I/O. Bots
// are server-visible integrations — an event delivered to a bot's webhook is not
// E2EE (the user chose to talk to the bot); user↔user interactive messages ride
// the sealed body on the client.
package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const (
	MaxName    = 60
	MaxButtons = 3
	MaxText    = 1000
)

var handleRe = regexp.MustCompile(`^[a-z0-9_]{3,32}$`)

var (
	ErrBadHandle  = errors.New("bots: handle must be 3–32 chars of a–z, 0–9, _")
	ErrBadName    = errors.New("bots: name is required (max 60 chars)")
	ErrBadWebhook = errors.New("bots: webhook must be an https URL")
	ErrBadText    = errors.New("bots: message text is required (max 1000 chars)")
	ErrBadButtons = errors.New("bots: an interactive message needs 1–3 buttons")
)

// ValidateHandle checks a bot's public @handle.
func ValidateHandle(h string) error {
	if !handleRe.MatchString(h) {
		return ErrBadHandle
	}
	return nil
}

// ValidateName checks a bot's display name.
func ValidateName(n string) error {
	if strings.TrimSpace(n) == "" || len(n) > MaxName {
		return ErrBadName
	}
	return nil
}

// ValidateWebhookURL requires an https endpoint (events carry a signature, but
// TLS protects them in transit).
func ValidateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ErrBadWebhook
	}
	return nil
}

// ValidateInteractive checks an interactive message's shape (server-side mirror of
// the client @wa/client-core validation).
func ValidateInteractive(text string, buttonCount int) error {
	if strings.TrimSpace(text) == "" || len(text) > MaxText {
		return ErrBadText
	}
	if buttonCount < 1 || buttonCount > MaxButtons {
		return ErrBadButtons
	}
	return nil
}

// Sign returns the hex HMAC-SHA256 of payload under secret — the value we send in
// the X-WA-Signature header so a bot can authenticate the event.
func Sign(secret, payload []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}

// VerifySignature constant-time compares a presented hex signature.
func VerifySignature(secret, payload []byte, sig string) bool {
	want := Sign(secret, payload)
	return hmac.Equal([]byte(want), []byte(sig))
}
