// Package backups owns encrypted-backup orchestration (media-svc, FR-SYNC-04):
// a presigned multipart upload for the client-encrypted archive, a 1-per-user
// registry, and a presigned restore GET. Blobs never transit this service —
// clients ↔ MinIO directly via presigned URLs; the server holds ciphertext refs
// + size only, never the Argon2id-derived key.
package backups

import (
	"context"
	"errors"
	"time"

	"github.com/whatsapp-v2/server/internal/media"
)

// ErrNotFound is returned by the store when no backup matches.
var ErrNotFound = errors.New("backups: not found")

// Backup is a backups row.
type Backup struct {
	ID        string
	UserID    string
	ObjectKey string
	SizeBytes int64
	Handle    string // MinIO multipart uploadId (pending only)
	CreatedAt time.Time
}

// CreateResult is the POST /backups response — presigned multipart PUT URLs.
type CreateResult struct {
	BackupID  string          `json:"backup_id"`
	ObjectKey string          `json:"object_key"`
	PartURLs  []media.PartURL `json:"part_urls"`
	PartSize  int64           `json:"part_size"`
}

// CompleteResult is the POST /backups/{id}/complete response.
type CompleteResult struct {
	BackupID string `json:"backup_id"`
}

// LatestResult is the GET /backups/latest response — a presigned restore GET.
type LatestResult struct {
	BackupID  string `json:"backup_id"`
	URL       string `json:"url"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt int64  `json:"created_at_ms"`
}

// Store persists the backup registry. One COMPLETE backup per user survives (a
// new completed backup replaces the old).
type Store interface {
	CreatePending(ctx context.Context, b Backup) error
	Get(ctx context.Context, id string) (Backup, error) // ErrNotFound; any state
	MarkComplete(ctx context.Context, id string) error
	// Latest returns the user's single complete backup, or ErrNotFound.
	Latest(ctx context.Context, userID string) (Backup, error)
	// OldComplete lists the user's other complete backups (to reclaim after a new
	// one completes — the 1-active-backup quota).
	OldComplete(ctx context.Context, userID, exceptID string) ([]Backup, error)
	Delete(ctx context.Context, id string) error
}

// Objects is the MinIO multipart seam — the same port media uploads use, so the
// one MinIO adapter backs both (blobs go client↔MinIO directly).
type Objects = media.Objects
