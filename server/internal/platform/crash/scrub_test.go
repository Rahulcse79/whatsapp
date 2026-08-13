package crash

import (
	"strings"
	"testing"
)

func TestScrubber_Text(t *testing.T) {
	s := NewScrubber()
	cases := []struct {
		name, in, want, leak string
	}{
		{"jwt", "auth eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.abc-DEF_123 here", "[redacted-jwt]", "eyJhbGci"},
		{"bearer", "Authorization: Bearer aBc123.def_456", "Bearer [redacted-token]", "aBc123.def"},
		{"email", "reached user.name@example.co.uk today", "[redacted-email]", "example.co.uk"},
		{"uuid", "device 0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b lost", "[redacted-uuid]", "0190a1b2"},
		{"ipv4", "peer 192.168.10.200 dropped", "[redacted-ip]", "192.168.10.200"},
		{"phone", "ring +14155550123 back", "[redacted-phone]", "+14155550123"},
	}
	for _, c := range cases {
		out := s.Text(c.in)
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: %q should contain %q", c.name, out, c.want)
		}
		if strings.Contains(out, c.leak) {
			t.Errorf("%s: %q still leaks %q", c.name, out, c.leak)
		}
	}
}

func TestScrubber_Tags_DropsSensitiveKeysAndScrubsValues(t *testing.T) {
	s := NewScrubber()
	in := map[string]string{
		"Authorization": "Bearer secret",
		"phone":         "+14155550123",
		"note":          "user bob@x.com from 10.0.0.1",
		"screen":        "chats",
	}
	out := s.Tags(in)

	if out["Authorization"] != "[redacted]" || out["phone"] != "[redacted]" {
		t.Fatalf("sensitive keys not dropped: %+v", out)
	}
	if strings.Contains(out["note"], "bob@x.com") || strings.Contains(out["note"], "10.0.0.1") {
		t.Errorf("note leaks PII: %q", out["note"])
	}
	if out["screen"] != "chats" {
		t.Errorf("benign value altered: %q", out["screen"])
	}
	// The scrubber must not mutate its input.
	if in["Authorization"] != "Bearer secret" || in["note"] != "user bob@x.com from 10.0.0.1" {
		t.Error("input map was mutated")
	}
}
