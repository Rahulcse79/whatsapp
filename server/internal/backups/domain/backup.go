// Package domain holds encrypted-backup pure logic: the per-profile size cap and
// multipart math. No I/O. The archive is client-encrypted under an Argon2id-
// derived key (the key never leaves the user); this service only ever sees
// ciphertext + size (FR-SYNC-04, relay-model ADR-001).
package domain

import "errors"

const (
	// PartSize is the multipart chunk (≥ 5 MB required by S3/MinIO; 8 MB matches
	// media uploads).
	PartSize = 8 * 1024 * 1024
	// DefaultMaxSize is the per-user backup cap (media-svc-lld §3: 2 GB default,
	// flag-tunable per deployment profile).
	DefaultMaxSize = 2 * 1024 * 1024 * 1024
)

var (
	ErrEmpty    = errors.New("backup: empty archive")
	ErrTooLarge = errors.New("backup: exceeds the size cap")
)

// ValidateSize checks a requested backup size against the cap.
func ValidateSize(size, max int64) error {
	switch {
	case size <= 0:
		return ErrEmpty
	case size > max:
		return ErrTooLarge
	default:
		return nil
	}
}

// NumParts is how many PartSize chunks a size needs (≥ 1 for a non-empty backup).
func NumParts(size int64) int {
	if size <= 0 {
		return 0
	}
	return int((size + PartSize - 1) / PartSize)
}
