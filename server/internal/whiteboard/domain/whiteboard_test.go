package domain

import "testing"

func TestValidateOp(t *testing.T) {
	if err := ValidateOp("op1", "stroke", 1, 100); err != nil {
		t.Fatalf("valid op rejected: %v", err)
	}
	if err := ValidateOp("", "stroke", 1, 10); err != ErrBadID {
		t.Fatal("missing id should fail")
	}
	if err := ValidateOp("op1", "scribble", 1, 10); err != ErrBadKind {
		t.Fatal("unknown kind should fail")
	}
	if err := ValidateOp("op1", "clear", 0, 10); err != ErrBadSeq {
		t.Fatal("non-positive seq should fail")
	}
	if err := ValidateOp("op1", "stroke", 1, MaxDataBytes+1); err != ErrBadData {
		t.Fatal("oversized payload should fail")
	}
}

func TestKindValid(t *testing.T) {
	for _, k := range []string{"stroke", "erase", "clear"} {
		if !KindValid(k) {
			t.Fatalf("%q should be valid", k)
		}
	}
	if KindValid("nope") {
		t.Fatal("unknown kind valid?")
	}
}
