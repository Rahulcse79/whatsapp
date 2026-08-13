package adapters

import "testing"

// escapeLike must neutralise every LIKE/ILIKE metacharacter so an admin's search
// term is matched literally — a "%" or "_" in the query can't widen the scan into
// a wildcard. This is the T4.06 self-review fix (parity with the user-facing
// contacts search, which already escaped).
func TestEscapeLike(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"alice":       "alice",
		"%":           `\%`,
		"_":           `\_`,
		`\`:           `\\`,
		"100%_off":    `100\%\_off`,
		`a\%b`:        `a\\\%b`,
		"%%%":         `\%\%\%`,
		"user_name%2": `user\_name\%2`,
	}
	for in, want := range cases {
		if got := escapeLike(in); got != want {
			t.Errorf("escapeLike(%q) = %q, want %q", in, got, want)
		}
	}
}
