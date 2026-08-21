package domain

import "testing"

func TestValidateQuery(t *testing.T) {
	if err := ValidateQuery("a"); err != ErrBadQuery {
		t.Fatal("1-char query should fail")
	}
	if err := ValidateQuery("news"); err != nil {
		t.Fatalf("valid query rejected: %v", err)
	}
}

func TestMatchScoreOrdering(t *testing.T) {
	q := NormalizeQuery("Dev")
	exact := MatchScore(q, "dev", "", false)
	prefix := MatchScore(q, "developers", "", false)
	word := MatchScore(q, "Cool Dev Club", "", false)
	contains := MatchScore(q, "bigdevs", "", false)
	secondary := MatchScore(q, "news", "@devnews", false)
	none := MatchScore(q, "sports", "@sports", false)

	if !(exact > prefix && prefix > word && word > contains && contains > secondary && secondary > none) {
		t.Fatalf("ranking not monotonic: exact=%.2f prefix=%.2f word=%.2f contains=%.2f secondary=%.2f none=%.2f", exact, prefix, word, contains, secondary, none)
	}
	if none != 0 {
		t.Fatal("no match should score 0")
	}
}

func TestVerifiedBoost(t *testing.T) {
	q := NormalizeQuery("acme")
	if MatchScore(q, "acme", "", true) <= MatchScore(q, "acme", "", false) {
		t.Fatal("verified should boost")
	}
	// a boost never lifts a non-match above a match
	if MatchScore(q, "other", "", true) != 0 {
		t.Fatal("verified boost must not apply to a non-match")
	}
}

func TestKindValid(t *testing.T) {
	for _, k := range []Kind{KindChannel, KindCommunity, KindUser} {
		if !KindValid(k) {
			t.Fatalf("%q should be valid", k)
		}
	}
	if KindValid("group") {
		t.Fatal("unknown kind valid?")
	}
}
