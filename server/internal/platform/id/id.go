// Package id generates and validates UUIDv7 identifiers — the ID scheme for
// every entity and idempotency key in the system. UUIDv7's millisecond
// timestamp prefix gives B-tree index locality (DS&A doc §1); clients
// generate message UUIDs themselves and the server validates them here.
package id

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// New returns a fresh UUIDv7 as a canonical lowercase string.
//
// It panics only if the OS entropy source fails, which is unrecoverable for
// a service that mints security-relevant identifiers — the documented
// exception to the no-panics rule (coding-standards.md).
func New() string {
	return NewUUID().String()
}

// NewUUID returns a fresh UUIDv7.
func NewUUID() uuid.UUID {
	u, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("id: OS entropy source failed: %v", err))
	}
	return u
}

// Parse validates that s is a well-formed UUIDv7 (the only version accepted
// from clients — enforced at the accept path so index locality can't be
// poisoned by random v4 IDs).
func Parse(s string) (uuid.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("id: %w", err)
	}
	if u.Version() != 7 {
		return uuid.Nil, fmt.Errorf("id: %q is UUIDv%d, want v7", s, u.Version())
	}
	return u, nil
}

// TimeOf extracts the embedded millisecond timestamp of a UUIDv7.
func TimeOf(u uuid.UUID) time.Time {
	sec, nsec := u.Time().UnixTime()
	return time.Unix(sec, nsec)
}
