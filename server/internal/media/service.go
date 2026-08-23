package media

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/media/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// ErrNotFound is returned by the ports when no row/session/object matches.
var ErrNotFound = errors.New("media: not found")

const (
	PresignTTL  = time.Hour           // multipart PUT URL lifetime
	DownloadTTL = 15 * time.Minute    // short-TTL presigned GET
	RefTTL      = 30 * 24 * time.Hour // last_ref + 30 d (media_objects.expires_at)
	PendingTTL  = 24 * time.Hour      // a pending upload session lives 24 h
)

// Store persists media_objects (PostgreSQL).
type Store interface {
	CreatePending(ctx context.Context, o Object) error
	Get(ctx context.Context, id string) (Object, error)
	GetByKey(ctx context.Context, key string) (Object, error)
	MarkComplete(ctx context.Context, id string, expiresAt time.Time) error
	AddRef(ctx context.Context, key string) (int32, error)
	DecRef(ctx context.Context, key string) (int32, error)
	// SweepCandidates returns rows eligible for GC: refcount 0 AND (expired OR
	// pending-stale) (media-svc-lld §4).
	SweepCandidates(ctx context.Context, now time.Time, limit int) ([]Object, error)
	Delete(ctx context.Context, id string) error
}

// Objects is the MinIO seam. Blobs never transit media-svc — clients PUT/GET
// directly via these presigned URLs.
type Objects interface {
	StartUpload(ctx context.Context, key string, parts int, expires time.Duration) (handle string, urls []PartURL, err error)
	PresignParts(ctx context.Context, key, handle string, partNumbers []int, expires time.Duration) ([]PartURL, error)
	Complete(ctx context.Context, key, handle string, etags []PartETag) error
	Stat(ctx context.Context, key string) (int64, error)
	Hash(ctx context.Context, key string) ([]byte, error)
	PresignGet(ctx context.Context, key string, expires time.Duration) (string, error)
	Abort(ctx context.Context, key, handle string) error
	Remove(ctx context.Context, key string) error
}

// Delivery mints the URL a client actually fetches an object from (T15.04). It
// is split from Objects because the read path is the one a CDN fronts: with no
// CDN configured this is a MinIO presigned GET exactly as before, and with one
// it is an edge URL carrying a signed token. Blobs are E2EE ciphertext, so an
// intermediate cache never sees plaintext.
type Delivery interface {
	// DownloadURL returns a URL granting a time-limited GET of key.
	DownloadURL(ctx context.Context, key string, expires time.Duration) (string, error)
	// Cacheable reports whether an edge cache serves the URLs, so callers can
	// tell clients a URL is worth holding on to.
	Cacheable() bool
}

// Sessions holds transient pending-upload state (the MinIO handle) out-of-band
// (Valkey, 24h TTL) since media_objects has no upload-handle column.
type Sessions interface {
	Save(ctx context.Context, sess UploadSession, ttl time.Duration) error
	Load(ctx context.Context, uploadID string) (UploadSession, error)
	Delete(ctx context.Context, uploadID string) error
}

// Quota is the core-api-owned storage counter (gRPC QuotaService). Fail-closed:
// a quota error rejects the upload (uploads are retryable, quota escapes aren't).
type Quota interface {
	CheckAndReserve(ctx context.Context, userID string, bytes int64) (allowed bool, remaining int64, err error)
}

// Rate gates create-upload (30/hr/user, GCRA).
type Rate interface {
	Allow(ctx context.Context, key string) (bool, error)
}

// Events publishes media.lifecycle facts (drives refcounts + GC).
type Events interface {
	Uploaded(ctx context.Context, objectKey string)
	RefAdded(ctx context.Context, objectKey string, refcount int32)
	RefRemoved(ctx context.Context, objectKey string, refcount int32)
	Orphaned(ctx context.Context, objectKey string)
}

// Service orchestrates uploads, verification, quota, refcounts, and downloads.
type Service struct {
	store    Store
	objects  Objects
	delivery Delivery
	sessions Sessions
	quota    Quota
	rate     Rate
	events   Events
	now      func() time.Time
}

func NewService(store Store, objects Objects, sessions Sessions, quota Quota, rate Rate, events Events) *Service {
	s := &Service{store: store, objects: objects, sessions: sessions, quota: quota, rate: rate, events: events, now: time.Now}
	s.delivery = directDelivery{objects} // no CDN configured: presign as before
	return s
}

// WithDelivery swaps the download-URL minter — this is how a CDN is put in
// front of MinIO without touching the upload path (T15.04). Passing nil keeps
// the direct presign.
func (s *Service) WithDelivery(d Delivery) *Service {
	if d != nil {
		s.delivery = d
	}
	return s
}

// directDelivery is the no-CDN default: a MinIO presigned GET, i.e. exactly the
// behaviour before T15.04.
type directDelivery struct{ objects Objects }

func (d directDelivery) DownloadURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	return d.objects.PresignGet(ctx, key, expires)
}
func (directDelivery) Cacheable() bool { return false }

// CreateUpload validates + rate-limits + reserves quota, opens a multipart
// upload, and returns presigned PUT URLs (POST /uploads).
func (s *Service) CreateUpload(ctx context.Context, ident auth.Identity, size int64, contentHash []byte, mime string) (CreateResult, error) {
	if err := domain.ValidateCreate(size); err != nil {
		if errors.Is(err, domain.ErrTooLarge) {
			return CreateResult{}, httpx.Reject(http.StatusRequestEntityTooLarge, "VALIDATION_TOO_LARGE", "file exceeds the 25MB cap")
		}
		return CreateResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_SIZE", "invalid size")
	}
	if len(contentHash) == 0 {
		return CreateResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_HASH", "content_hash required")
	}
	if ok, err := s.rate.Allow(ctx, "media-create:"+ident.UserID); err != nil {
		return CreateResult{}, httpx.Transient()
	} else if !ok {
		return CreateResult{}, httpx.Reject(http.StatusTooManyRequests, "RATE_LIMITED", "too many uploads (30/hr)")
	}
	allowed, _, err := s.quota.CheckAndReserve(ctx, ident.UserID, size)
	if err != nil {
		return CreateResult{}, httpx.Reject(http.StatusServiceUnavailable, "TRANSIENT_UNAVAILABLE", "quota check unavailable")
	}
	if !allowed {
		return CreateResult{}, httpx.Reject(http.StatusConflict, "STATE_QUOTA_EXCEEDED", "storage quota exceeded")
	}

	uploadID := id.New()
	key := ident.UserID + "/" + uploadID
	parts := domain.NumParts(size)
	handle, urls, err := s.objects.StartUpload(ctx, key, parts, PresignTTL)
	if err != nil {
		return CreateResult{}, httpx.Transient()
	}
	if err := s.store.CreatePending(ctx, Object{
		ID: uploadID, ObjectKey: key, SizeBytes: size, ContentHash: contentHash,
		UploaderID: ident.UserID, Refcount: 0, State: domain.StatePending, CreatedAt: s.now(),
	}); err != nil {
		_ = s.objects.Abort(ctx, key, handle)
		return CreateResult{}, httpx.Transient()
	}
	if err := s.sessions.Save(ctx, UploadSession{
		UploadID: uploadID, ObjectKey: key, Handle: handle, SizeBytes: size,
		ContentHash: contentHash, UploaderID: ident.UserID, Parts: parts,
	}, PendingTTL); err != nil {
		return CreateResult{}, httpx.Transient()
	}
	return CreateResult{UploadID: uploadID, ObjectKey: key, PartURLs: urls, PartSize: domain.PartSize}, nil
}

// PresignMissing re-issues URLs for missing parts (POST /uploads/{id}/presign
// — the resumable-upload primitive, FR-MED-04).
func (s *Service) PresignMissing(ctx context.Context, ident auth.Identity, uploadID string, missing []int) ([]PartURL, error) {
	sess, err := s.ownedSession(ctx, ident, uploadID)
	if err != nil {
		return nil, err
	}
	urls, err := s.objects.PresignParts(ctx, sess.ObjectKey, sess.Handle, missing, PresignTTL)
	if err != nil {
		return nil, httpx.Transient()
	}
	return urls, nil
}

// CompleteUpload finalises the multipart upload and verifies size (+ hash for
// single-part), then registers the object COMPLETE (POST /uploads/{id}/complete).
func (s *Service) CompleteUpload(ctx context.Context, ident auth.Identity, uploadID string, etags []PartETag) (string, error) {
	sess, err := s.ownedSession(ctx, ident, uploadID)
	if err != nil {
		return "", err
	}
	if err := s.objects.Complete(ctx, sess.ObjectKey, sess.Handle, etags); err != nil {
		return "", httpx.Reject(http.StatusBadRequest, "VALIDATION_PARTS", "multipart completion failed")
	}
	size, err := s.objects.Stat(ctx, sess.ObjectKey)
	if err != nil {
		return "", httpx.Transient()
	}
	if size != sess.SizeBytes {
		return s.rejectAndCleanup(ctx, sess, "VALIDATION_SIZE_MISMATCH", "uploaded size does not match")
	}
	if domain.Single(sess.SizeBytes) {
		hash, err := s.objects.Hash(ctx, sess.ObjectKey)
		if err != nil {
			return "", httpx.Transient()
		}
		if !bytes.Equal(hash, sess.ContentHash) {
			return s.rejectAndCleanup(ctx, sess, "VALIDATION_HASH_MISMATCH", "content hash mismatch")
		}
	}
	if err := s.store.MarkComplete(ctx, uploadID, s.now().Add(RefTTL)); err != nil {
		return "", httpx.Transient()
	}
	_ = s.sessions.Delete(ctx, uploadID)
	s.events.Uploaded(ctx, sess.ObjectKey)
	return uploadID, nil
}

// DownloadURLs mints short-TTL presigned GETs for complete objects
// (POST /download-urls). Unknown/incomplete keys are silently skipped.
func (s *Service) DownloadURLs(ctx context.Context, ident auth.Identity, keys []string) ([]DownloadURL, error) {
	out := make([]DownloadURL, 0, len(keys))
	expiresMS := s.now().Add(DownloadTTL).UnixMilli()
	for _, key := range keys {
		obj, err := s.store.GetByKey(ctx, key)
		if errors.Is(err, ErrNotFound) || obj.State != domain.StateComplete {
			continue
		}
		if err != nil {
			return nil, httpx.Transient()
		}
		url, err := s.delivery.DownloadURL(ctx, key, DownloadTTL)
		if err != nil {
			return nil, httpx.Transient()
		}
		out = append(out, DownloadURL{
			Key:       key,
			URL:       url,
			Expires:   expiresMS,
			SizeBytes: obj.SizeBytes,
			Cached:    s.delivery.Cacheable(),
		})
	}
	// NOTE: membership-of-referencing-conversation check is enforced when the
	// chat gRPC surface is wired (media-stories-api.md).
	return out, nil
}

// AddRef increments an object's refcount when a message referencing it is
// accepted (chat gRPC). Refcount is the single-writer truth.
func (s *Service) AddRef(ctx context.Context, objectKey string) error {
	rc, err := s.store.AddRef(ctx, objectKey)
	if errors.Is(err, ErrNotFound) {
		return httpx.Reject(http.StatusNotFound, "MEDIA_NOT_FOUND", "no such object")
	}
	if err != nil {
		return httpx.Transient()
	}
	s.events.RefAdded(ctx, objectKey, rc)
	return nil
}

// DecRef decrements refcount (delete-for-everyone, account purge via
// media.lifecycle). At 0 the object becomes GC-eligible after expires_at.
func (s *Service) DecRef(ctx context.Context, objectKey string) error {
	rc, err := s.store.DecRef(ctx, objectKey)
	if errors.Is(err, ErrNotFound) {
		return nil // already gone — idempotent
	}
	if err != nil {
		return httpx.Transient()
	}
	s.events.RefRemoved(ctx, objectKey, rc)
	return nil
}

func (s *Service) ownedSession(ctx context.Context, ident auth.Identity, uploadID string) (UploadSession, error) {
	sess, err := s.sessions.Load(ctx, uploadID)
	if errors.Is(err, ErrNotFound) {
		return UploadSession{}, httpx.Reject(http.StatusNotFound, "UPLOAD_NOT_FOUND", "no such upload")
	}
	if err != nil {
		return UploadSession{}, httpx.Transient()
	}
	if sess.UploaderID != ident.UserID {
		return UploadSession{}, httpx.Reject(http.StatusForbidden, "STATE_FORBIDDEN", "not your upload")
	}
	return sess, nil
}

func (s *Service) rejectAndCleanup(ctx context.Context, sess UploadSession, code, msg string) (string, error) {
	_ = s.objects.Remove(ctx, sess.ObjectKey)
	_ = s.store.Delete(ctx, sess.UploadID)
	_ = s.sessions.Delete(ctx, sess.UploadID)
	return "", httpx.Reject(http.StatusUnprocessableEntity, code, msg)
}
