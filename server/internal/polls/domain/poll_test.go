package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestValidateCreate(t *testing.T) {
	for _, n := range []int{2, 7, 12} {
		if err := ValidateCreate(n); err != nil {
			t.Errorf("ValidateCreate(%d) = %v, want nil", n, err)
		}
	}
	for _, n := range []int{0, 1, 13, 100} {
		if !errors.Is(ValidateCreate(n), ErrOptionCount) {
			t.Errorf("ValidateCreate(%d) should reject", n)
		}
	}
}

func TestValidateVote(t *testing.T) {
	tests := []struct {
		name    string
		indices []int
		count   int
		multi   bool
		want    error
	}{
		{"single ok", []int{1}, 3, false, nil},
		{"multi ok", []int{0, 2}, 3, true, nil},
		{"empty", nil, 3, false, ErrNoVote},
		{"single with two", []int{0, 1}, 3, false, ErrSingleChoice},
		{"out of range", []int{3}, 3, false, ErrBadIndex},
		{"negative", []int{-1}, 3, true, ErrBadIndex},
		{"duplicate", []int{1, 1}, 3, true, ErrDupIndex},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateVote(tc.indices, tc.count, tc.multi); !errors.Is(err, tc.want) {
				t.Errorf("ValidateVote = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTally(t *testing.T) {
	// Two voters on index 0, one on index 2 (a multi voter contributes to each).
	got := Tally([]int{0, 0, 2, 1}, 3)
	if want := []int{2, 1, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("Tally = %v, want %v", got, want)
	}
	// Out-of-range rows are ignored defensively.
	if got := Tally([]int{5}, 3); !reflect.DeepEqual(got, []int{0, 0, 0}) {
		t.Errorf("Tally ignored range: %v", got)
	}
}
