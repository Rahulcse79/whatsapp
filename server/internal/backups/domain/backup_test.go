package domain

import (
	"errors"
	"testing"
)

func TestValidateSize(t *testing.T) {
	const max = 2 * 1024 * 1024
	if err := ValidateSize(1000, max); err != nil {
		t.Fatalf("in-range: %v", err)
	}
	if err := ValidateSize(max, max); err != nil {
		t.Fatalf("exactly max: %v", err)
	}
	if err := ValidateSize(0, max); !errors.Is(err, ErrEmpty) {
		t.Fatalf("zero → %v, want ErrEmpty", err)
	}
	if err := ValidateSize(max+1, max); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over max → %v, want ErrTooLarge", err)
	}
}

func TestNumParts(t *testing.T) {
	cases := map[int64]int{0: 0, 1: 1, PartSize: 1, PartSize + 1: 2, 3*PartSize - 1: 3}
	for size, want := range cases {
		if got := NumParts(size); got != want {
			t.Fatalf("NumParts(%d) = %d, want %d", size, got, want)
		}
	}
}
