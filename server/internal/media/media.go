// Package media owns upload orchestration (presigned multipart), completion
// verification, quota/rate enforcement, download-URL minting, refcounts, and GC
// of the media_objects registry. Blobs never transit this service — clients ↔
// MinIO directly via presigned URLs (media-svc-lld.md, HLD §9).
package media

import (
	"time"

	"github.com/whatsapp-v2/server/internal/media/domain"
)

// Object is a media_objects row.
type Object struct {
	ID          string
	ObjectKey   string
	SizeBytes   int64
	ContentHash []byte
	UploaderID  string
	Refcount    int32
	State       domain.UploadState
	CreatedAt   time.Time
	ExpiresAt   *time.Time
}

// UploadSession is the transient state for a pending multipart upload (the
// MinIO handle + the expected size/hash), held out-of-band (Valkey, 24h TTL)
// because media_objects has no upload-handle column.
type UploadSession struct {
	UploadID    string
	ObjectKey   string
	Handle      string // MinIO multipart uploadId
	SizeBytes   int64
	ContentHash []byte
	UploaderID  string
	Parts       int
}

// PartURL is one presigned multipart PUT URL.
type PartURL struct {
	PartNumber int    `json:"part_number"`
	URL        string `json:"url"`
}

// PartETag is a completed part's number + ETag (from the client's direct PUTs).
type PartETag struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

// CreateResult is the POST /uploads response.
type CreateResult struct {
	UploadID  string    `json:"upload_id"`
	ObjectKey string    `json:"object_key"`
	PartURLs  []PartURL `json:"part_urls"`
	PartSize  int64     `json:"part_size"`
}

// DownloadURL is one short-TTL presigned GET.
type DownloadURL struct {
	Key     string `json:"key"`
	URL     string `json:"url"`
	Expires int64  `json:"expires_ms"`
	// SizeBytes lets a client choose between a progressive/ranged fetch and a
	// plain download without a HEAD round-trip. The server cannot report a
	// content type — the blob is ciphertext and only the envelope knows.
	SizeBytes int64 `json:"size_bytes"`
	// Cached is true when an edge cache serves the URL, so a client may reuse
	// it for its whole TTL instead of re-minting per attempt.
	Cached bool `json:"cached,omitempty"`
}
