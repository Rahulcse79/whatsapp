package domain

import (
	"crypto/rand"
	"testing"
	"time"
)

func TestEvaluateSession_Matrix(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	active := Session{ID: "s1", ExpiresAt: now.Add(time.Hour)}

	cases := []struct {
		name string
		s    Session
		want RefreshOutcome
	}{
		{"active rotates", active, RefreshRotate},
		{"expired", Session{ID: "s2", ExpiresAt: now.Add(-time.Second)}, RefreshExpired},
		{"revoked", Session{ID: "s3", ExpiresAt: now.Add(time.Hour), RevokedAt: now.Add(-time.Minute)}, RefreshRevoked},
		{"revoked wins over expired", Session{ID: "s4", ExpiresAt: now.Add(-time.Hour), RevokedAt: now.Add(-time.Minute)}, RefreshRevoked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateSession(tc.s, now); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewRefreshToken(t *testing.T) {
	tok1, h1, err := NewRefreshToken(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tok2, _, _ := NewRefreshToken(rand.Reader)
	if tok1 == tok2 {
		t.Fatal("tokens must be unique")
	}
	if len(h1) != 32 {
		t.Fatalf("hash length %d, want 32 (sha-256)", len(h1))
	}
	if string(HashToken(tok1)) != string(h1) {
		t.Fatal("HashToken(token) must equal the hash returned at mint time")
	}
	if len(tok1) != 43 { // 32 bytes base64url, unpadded
		t.Fatalf("token length %d, want 43", len(tok1))
	}
}
