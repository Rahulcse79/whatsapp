// Package crash is the self-hosted crash-reporting layer (HLD §18.1): payloads
// are scrubbed of PII before they ever leave the process, and a crash-free
// ratio feeds the product dashboard. The backend (GlitchTip/Sentry) is infra;
// this package owns the scrubbing + the ratio, which are the parts worth code.
package crash

import (
	"regexp"
	"strings"
)

// redaction patterns, most-specific first (tokens before the generic scrubs, so
// a JWT isn't half-eaten by the UUID rule). Every match becomes a typed
// placeholder, never the original value.
var scrubs = []struct {
	re   *regexp.Regexp
	with string
}{
	// JWTs (three base64url segments, header starts with eyJ).
	{regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`), "[redacted-jwt]"},
	// Authorization: Bearer <token>.
	{regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`), "Bearer [redacted-token]"},
	// Emails.
	{regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), "[redacted-email]"},
	// UUIDs (user/device/session ids).
	{regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`), "[redacted-uuid]"},
	// IPv4 addresses.
	{regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`), "[redacted-ip]"},
	// E.164 phone numbers (leading +), then long bare digit runs.
	{regexp.MustCompile(`\+\d{7,15}\b`), "[redacted-phone]"},
	{regexp.MustCompile(`\b\d{10,15}\b`), "[redacted-phone]"},
}

// sensitiveKeys are dropped wholesale from tag/context maps — their values are
// PII or secrets regardless of shape.
var sensitiveKeys = map[string]bool{
	"authorization": true, "cookie": true, "set-cookie": true,
	"password": true, "pin": true, "token": true, "refresh_token": true,
	"phone": true, "phone_number": true, "email": true, "otp": true,
}

// Scrubber redacts PII from crash payloads. It holds no state; a single value is
// safe for concurrent use.
type Scrubber struct{}

func NewScrubber() *Scrubber { return &Scrubber{} }

// Text applies every redaction to a free-text field (message, breadcrumb, stack
// frame). Order is fixed so specific patterns win over general ones.
func (s *Scrubber) Text(in string) string {
	for _, sc := range scrubs {
		in = sc.re.ReplaceAllString(in, sc.with)
	}
	return in
}

// Tags scrubs a context/tag map: a sensitive key is dropped entirely, every
// other value is Text-scrubbed. The input is not mutated.
func (s *Scrubber) Tags(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if sensitiveKeys[strings.ToLower(k)] {
			out[k] = "[redacted]"
			continue
		}
		out[k] = s.Text(v)
	}
	return out
}
