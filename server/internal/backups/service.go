package backups

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/backups/domain"
	"github.com/whatsapp-v2/server/internal/media"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

const (
	presignTTL  = time.Hour        // multipart PUT URL lifetime
	downloadTTL = 15 * time.Minute // short-TTL restore GET
)

// Service orchestrates backup upload (presigned multipart), completion, and the
// restore GET, enforcing the per-user size cap and the 1-active-backup quota.
type Service struct {
	objects Objects
	store   Store
	maxSize int64
	now     func() time.Time
	newID   func() string
}

func NewService(objects Objects, store Store, maxSize int64) *Service {
	if maxSize <= 0 {
		maxSize = domain.DefaultMaxSize
	}
	return &Service{objects: objects, store: store, maxSize: maxSize, now: time.Now, newID: id.New}
}

// Create opens a multipart upload for a client-encrypted archive of `size` bytes
// and returns presigned PUT URLs (POST /backups).
func (s *Service) Create(ctx context.Context, ident auth.Identity, size int64) (CreateResult, error) {
	if err := domain.ValidateSize(size, s.maxSize); err != nil {
		if errors.Is(err, domain.ErrTooLarge) {
			return CreateResult{}, httpx.Reject(http.StatusRequestEntityTooLarge, "VALIDATION_TOO_LARGE", "backup exceeds the size cap")
		}
		return CreateResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_SIZE", "invalid size")
	}

	backupID := s.newID()
	key := "backups/" + ident.UserID + "/" + backupID
	handle, urls, err := s.objects.StartUpload(ctx, key, domain.NumParts(size), presignTTL)
	if err != nil {
		return CreateResult{}, httpx.Transient()
	}
	if err := s.store.CreatePending(ctx, Backup{
		ID: backupID, UserID: ident.UserID, ObjectKey: key, SizeBytes: size, Handle: handle, CreatedAt: s.now(),
	}); err != nil {
		_ = s.objects.Abort(ctx, key, handle)
		return CreateResult{}, httpx.Transient()
	}
	return CreateResult{BackupID: backupID, ObjectKey: key, PartURLs: urls, PartSize: domain.PartSize}, nil
}

// Complete finalizes the multipart upload, verifies the byte count, marks the
// backup complete, and reclaims the user's previous backup (1-active quota).
func (s *Service) Complete(ctx context.Context, ident auth.Identity, backupID string, etags []media.PartETag) (CompleteResult, error) {
	b, err := s.owned(ctx, ident, backupID)
	if err != nil {
		return CompleteResult{}, err
	}
	if err := s.objects.Complete(ctx, b.ObjectKey, b.Handle, etags); err != nil {
		return CompleteResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_PARTS", "multipart completion failed")
	}
	size, err := s.objects.Stat(ctx, b.ObjectKey)
	if err != nil {
		return CompleteResult{}, httpx.Transient()
	}
	if size != b.SizeBytes {
		_ = s.objects.Remove(ctx, b.ObjectKey)
		_ = s.store.Delete(ctx, backupID)
		return CompleteResult{}, httpx.Reject(http.StatusUnprocessableEntity, "VALIDATION_SIZE_MISMATCH", "uploaded size does not match")
	}
	if err := s.store.MarkComplete(ctx, backupID); err != nil {
		return CompleteResult{}, httpx.Transient()
	}
	s.reclaimOld(ctx, ident.UserID, backupID)
	return CompleteResult{BackupID: backupID}, nil
}

// Latest returns a presigned restore GET for the user's current backup, or 404.
func (s *Service) Latest(ctx context.Context, ident auth.Identity) (LatestResult, error) {
	b, err := s.store.Latest(ctx, ident.UserID)
	if errors.Is(err, ErrNotFound) {
		return LatestResult{}, httpx.Reject(http.StatusNotFound, "NO_BACKUP", "no backup for this user")
	}
	if err != nil {
		return LatestResult{}, httpx.Transient()
	}
	url, err := s.objects.PresignGet(ctx, b.ObjectKey, downloadTTL)
	if err != nil {
		return LatestResult{}, httpx.Transient()
	}
	return LatestResult{BackupID: b.ID, URL: url, SizeBytes: b.SizeBytes, CreatedAt: b.CreatedAt.UnixMilli()}, nil
}

func (s *Service) owned(ctx context.Context, ident auth.Identity, backupID string) (Backup, error) {
	b, err := s.store.Get(ctx, backupID)
	if errors.Is(err, ErrNotFound) {
		return Backup{}, httpx.Reject(http.StatusNotFound, "BACKUP_NOT_FOUND", "no such backup")
	}
	if err != nil {
		return Backup{}, httpx.Transient()
	}
	if b.UserID != ident.UserID {
		return Backup{}, httpx.Reject(http.StatusForbidden, "STATE_FORBIDDEN", "not your backup")
	}
	return b, nil
}

// reclaimOld deletes the user's previous complete backups (blob + row). Best
// effort: the new backup is already durable; a stale old blob is caught by GC.
func (s *Service) reclaimOld(ctx context.Context, userID, exceptID string) {
	old, err := s.store.OldComplete(ctx, userID, exceptID)
	if err != nil {
		return
	}
	for _, o := range old {
		_ = s.objects.Remove(ctx, o.ObjectKey)
		_ = s.store.Delete(ctx, o.ID)
	}
}
