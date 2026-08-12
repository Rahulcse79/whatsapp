package domain

import (
	"errors"
	"testing"
	"time"
)

func TestParseKind(t *testing.T) {
	for s, want := range map[string]Kind{"image": KindImage, "video": KindVideo, "text": KindText} {
		if k, ok := ParseKind(s); !ok || k != want {
			t.Fatalf("ParseKind(%q) = %v,%v", s, k, ok)
		}
	}
	if _, ok := ParseKind("gif"); ok {
		t.Fatal("unknown kind should not parse")
	}
}

func TestValidateCreate(t *testing.T) {
	if err := ValidateCreate(KindImage, true, 3); err != nil {
		t.Fatalf("image+media+audience: %v", err)
	}
	if err := ValidateCreate(KindText, false, 3); err != nil {
		t.Fatalf("text+no-media: %v", err)
	}
	if err := ValidateCreate(KindImage, false, 3); !errors.Is(err, ErrMediaMissing) {
		t.Fatalf("image w/o media → %v, want ErrMediaMissing", err)
	}
	if err := ValidateCreate(KindText, true, 3); !errors.Is(err, ErrMediaOnText) {
		t.Fatalf("text w/ media → %v, want ErrMediaOnText", err)
	}
	if err := ValidateCreate(KindImage, true, 0); !errors.Is(err, ErrNoAudience) {
		t.Fatalf("empty audience → %v, want ErrNoAudience", err)
	}
	if err := ValidateCreate(Kind(9), false, 3); !errors.Is(err, ErrBadKind) {
		t.Fatalf("bad kind → %v, want ErrBadKind", err)
	}
}

func TestExpiryFrom(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	if got := ExpiryFrom(now); !got.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("expiry = %v, want now+24h", got)
	}
}

func TestAudience(t *testing.T) {
	// Default: the author's contacts, plus the author, deduped.
	got := Audience("author", nil, []string{"a", "b", "a", "author"})
	if len(got) != 3 || got[0] != "author" {
		t.Fatalf("default audience = %v, want [author a b]", got)
	}

	// Override replaces contacts (author still included).
	got = Audience("author", []string{"x", "y"}, []string{"a", "b"})
	set := map[string]bool{}
	for _, id := range got {
		set[id] = true
	}
	if !set["author"] || !set["x"] || !set["y"] || set["a"] {
		t.Fatalf("override audience = %v, want {author,x,y}", got)
	}

	// An empty override (explicit []) is not the same as nil: it means "just me".
	if got := Audience("author", []string{}, []string{"a"}); len(got) != 1 || got[0] != "author" {
		t.Fatalf("empty override = %v, want [author]", got)
	}
}
