// Package domain holds media-svc's pure logic: the upload state machine, the
// per-file cap, and multipart math. No I/O (depguard domain-stays-pure).
package domain

import "errors"

const (
	// MaxFileSize is the per-file cap (media-svc-lld §3, FR-MED). Blobs are
	// ciphertext; this bounds a single media object.
	MaxFileSize = 25 * 1024 * 1024 // 25 MB
	// PartSize is the multipart chunk. S3/MinIO require ≥ 5 MB per part except
	// the last; 8 MB keeps part counts small for a 25 MB cap.
	PartSize = 8 * 1024 * 1024
)

// UploadState mirrors media_objects.upload_state.
type UploadState int16

const (
	StatePending     UploadState = 0
	StateComplete    UploadState = 1
	StateGCCandidate UploadState = 2
)

var (
	ErrEmpty    = errors.New("media: empty object")
	ErrTooLarge = errors.New("media: object exceeds the 25MB cap")
)

// ValidateCreate checks a requested upload size against the per-file cap.
func ValidateCreate(size int64) error {
	switch {
	case size <= 0:
		return ErrEmpty
	case size > MaxFileSize:
		return ErrTooLarge
	default:
		return nil
	}
}

// NumParts returns how many multipart parts a size needs (1-based count).
func NumParts(size int64) int {
	if size <= 0 {
		return 0
	}
	return int((size + PartSize - 1) / PartSize)
}

// PartByteRange returns the [start, end) byte range of part i (0-based). end is
// clamped to size, so the last part may be shorter than PartSize.
func PartByteRange(size int64, i int) (start, end int64) {
	start = int64(i) * PartSize
	end = start + PartSize
	if end > size {
		end = size
	}
	return start, end
}

// Single reports whether the upload is a single-part upload (≤ PartSize), which
// media-svc can hash-verify server-side via a ranged GET (§2).
func Single(size int64) bool { return NumParts(size) <= 1 }
