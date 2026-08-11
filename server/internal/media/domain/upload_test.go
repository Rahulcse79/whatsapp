package domain

import (
	"errors"
	"testing"
)

func TestValidateCreate(t *testing.T) {
	if err := ValidateCreate(0); !errors.Is(err, ErrEmpty) {
		t.Fatal("zero size must be empty")
	}
	if err := ValidateCreate(MaxFileSize); err != nil {
		t.Fatalf("exactly 25MB must be allowed: %v", err)
	}
	if err := ValidateCreate(MaxFileSize + 1); !errors.Is(err, ErrTooLarge) {
		t.Fatal("over-cap must be rejected")
	}
}

func TestNumPartsAndSingle(t *testing.T) {
	cases := []struct {
		size  int64
		parts int
		one   bool
	}{
		{0, 0, true},
		{1, 1, true},
		{PartSize, 1, true},
		{PartSize + 1, 2, false},
		{MaxFileSize, 4, false}, // ceil(25MB / 8MB) = 4
	}
	for _, c := range cases {
		if got := NumParts(c.size); got != c.parts {
			t.Fatalf("NumParts(%d) = %d, want %d", c.size, got, c.parts)
		}
		if got := Single(c.size); got != c.one {
			t.Fatalf("Single(%d) = %v, want %v", c.size, got, c.one)
		}
	}
}

func TestPartByteRange(t *testing.T) {
	size := int64(PartSize + 100)
	s0, e0 := PartByteRange(size, 0)
	if s0 != 0 || e0 != PartSize {
		t.Fatalf("part 0 = [%d,%d), want [0,%d)", s0, e0, PartSize)
	}
	s1, e1 := PartByteRange(size, 1)
	if s1 != PartSize || e1 != size {
		t.Fatalf("part 1 = [%d,%d), want [%d,%d) (short last part)", s1, e1, PartSize, size)
	}
}
