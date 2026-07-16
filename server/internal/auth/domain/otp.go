// Package domain holds the auth context's pure logic: OTP codes and
// verification decisions, PIN hashing, refresh-token rotation semantics.
// No I/O imports — enforced by depguard (domain-stays-pure).
package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// OTP policy (FR-AUTH-02).
const (
	OTPValidity    = 10 * time.Minute
	OTPMaxAttempts = 5
)

// Channel is how an OTP reaches the user.
type Channel int16

const (
	ChannelSMS Channel = iota
	ChannelEmail
	ChannelMock // dev only — config guard forbids it in prod
)

func (c Channel) String() string {
	switch c {
	case ChannelSMS:
		return "sms"
	case ChannelEmail:
		return "email"
	case ChannelMock:
		return "mock"
	default:
		return "unknown"
	}
}

// Challenge is one OTP round: created at request-otp, resolved at verify.
type Challenge struct {
	ID         string
	PhoneHash  []byte
	CodeHash   []byte
	Salt       []byte
	Channel    Channel
	Attempts   int
	VerifiedAt time.Time // zero until the code was accepted
	PinPending bool      // OTP passed but the 2FA PIN gate remains
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// NewCode draws an unbiased 6-digit code. Rejection sampling avoids the
// modulo bias a naive n%10^6 would carry.
func NewCode(entropy io.Reader) (string, error) {
	const limit = 4_294_000_000 // largest multiple of 10^6 that fits uint32
	var buf [4]byte
	for {
		if _, err := io.ReadFull(entropy, buf[:]); err != nil {
			return "", fmt.Errorf("otp entropy: %w", err)
		}
		if n := binary.BigEndian.Uint32(buf[:]); n < limit {
			return fmt.Sprintf("%06d", n%1_000_000), nil
		}
	}
}

// HashCode binds a code to its challenge salt. HMAC-SHA256 is sufficient for
// 6-digit codes because brute force is bounded by OTPMaxAttempts and the
// 10-minute TTL — not by hash cost (unlike PINs, see pin.go).
func HashCode(salt []byte, code string) []byte {
	m := hmac.New(sha256.New, salt)
	m.Write([]byte(code))
	return m.Sum(nil)
}

// VerifyOutcome classifies a code submission.
type VerifyOutcome int

const (
	VerifyOK VerifyOutcome = iota
	VerifyWrongCode
	VerifyExpired
	VerifyTooManyAttempts
	VerifyAlreadyUsed
)

// VerifyCode is the pure decision — it inspects, never mutates. The caller
// persists the attempt increment or verified flag per the outcome.
func VerifyCode(ch Challenge, code string, now time.Time) VerifyOutcome {
	if !ch.VerifiedAt.IsZero() {
		return VerifyAlreadyUsed
	}
	if now.After(ch.ExpiresAt) {
		return VerifyExpired
	}
	if ch.Attempts >= OTPMaxAttempts {
		return VerifyTooManyAttempts
	}
	if hmac.Equal(ch.CodeHash, HashCode(ch.Salt, code)) {
		return VerifyOK
	}
	return VerifyWrongCode
}
