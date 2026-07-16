package domain

import (
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func testChallenge(code string, now time.Time) Challenge {
	salt := []byte("0123456789abcdef")
	return Challenge{
		ID:        "ch1",
		Salt:      salt,
		CodeHash:  HashCode(salt, code),
		CreatedAt: now,
		ExpiresAt: now.Add(OTPValidity),
	}
}

func TestNewCode_SixDigits(t *testing.T) {
	for i := 0; i < 100; i++ {
		code, err := NewCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
			t.Fatalf("code %q is not 6 digits", code)
		}
	}
}

func TestVerifyCode_Matrix(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	base := testChallenge("123456", now)

	cases := []struct {
		name string
		ch   func() Challenge
		code string
		at   time.Time
		want VerifyOutcome
	}{
		{"correct code", func() Challenge { return base }, "123456", now, VerifyOK},
		{"wrong code", func() Challenge { return base }, "654321", now, VerifyWrongCode},
		{"expired", func() Challenge { return base }, "123456", now.Add(OTPValidity + time.Second), VerifyExpired},
		{"at limit", func() Challenge { c := base; c.Attempts = OTPMaxAttempts; return c }, "123456", now, VerifyTooManyAttempts},
		{"already used", func() Challenge { c := base; c.VerifiedAt = now; return c }, "123456", now, VerifyAlreadyUsed},
		{"under limit still works", func() Challenge { c := base; c.Attempts = OTPMaxAttempts - 1; return c }, "123456", now, VerifyOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyCode(tc.ch(), tc.code, tc.at); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHashCode_SaltDependence(t *testing.T) {
	a := HashCode([]byte("salt-a"), "123456")
	b := HashCode([]byte("salt-b"), "123456")
	if string(a) == string(b) {
		t.Fatal("same code under different salts must hash differently")
	}
	if string(a) != string(HashCode([]byte("salt-a"), "123456")) {
		t.Fatal("hash must be deterministic for the same salt+code")
	}
}
