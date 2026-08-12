package domain

import (
	"testing"
	"time"
)

func TestNormalizeHandle(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		valid bool
	}{
		{"+14155550123", "+14155550123", true},
		{"  +14155550123  ", "+14155550123", true}, // trimmed
		{"14155550123", "14155550123", false},      // no leading +
		{"+0123456", "+0123456", false},            // leading 0 after +
		{"+1", "+1", false},                        // too short
		{"not a phone", "not a phone", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := NormalizeHandle(c.in)
		if got != c.want || ok != c.valid {
			t.Errorf("NormalizeHandle(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.valid)
		}
	}
}

func TestNormalizeQuery(t *testing.T) {
	if _, ok := NormalizeQuery(" a "); ok {
		t.Error("a 1-char query must be rejected (directory-scrape guard)")
	}
	if n, ok := NormalizeQuery("  al  "); !ok || n != "al" {
		t.Errorf("NormalizeQuery(\"  al  \") = (%q,%v), want (al,true)", n, ok)
	}
	// Length is counted in runes, not bytes: two multibyte runes are enough.
	if _, ok := NormalizeQuery("é😀"); !ok {
		t.Error("a 2-rune query should be accepted even though it is >2 bytes")
	}
}

func TestInviteValid(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	base := Invite{ExpiresAt: now.Add(time.Hour), MaxUses: 2, Uses: 0}

	if !base.Valid(now) {
		t.Fatal("a fresh, unexpired, unused invite should be valid")
	}
	if base.Valid(now.Add(2 * time.Hour)) {
		t.Error("an expired invite must be invalid")
	}
	if exhausted := (Invite{ExpiresAt: now.Add(time.Hour), MaxUses: 2, Uses: 2}); exhausted.Valid(now) {
		t.Error("an invite at its max-uses must be invalid")
	}
	revokedAt := now
	if revoked := (Invite{ExpiresAt: now.Add(time.Hour), MaxUses: 2, RevokedAt: &revokedAt}); revoked.Valid(now) {
		t.Error("a revoked invite must be invalid")
	}
	if unlimited := (Invite{ExpiresAt: now.Add(time.Hour), MaxUses: 0, Uses: 1000}); !unlimited.Valid(now) {
		t.Error("an unlimited (MaxUses 0) invite stays valid regardless of uses")
	}
}
