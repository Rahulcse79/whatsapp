package backups

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/backups/domain"
	"github.com/whatsapp-v2/server/internal/media"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeObjects struct {
	statSize int64
	removed  []string
	aborted  []string
}

func (o *fakeObjects) StartUpload(_ context.Context, key string, parts int, _ time.Duration) (string, []media.PartURL, error) {
	urls := make([]media.PartURL, parts)
	for i := 0; i < parts; i++ {
		urls[i] = media.PartURL{PartNumber: i + 1, URL: "put://" + key + "/" + string(rune('1'+i))}
	}
	return "handle-" + key, urls, nil
}
func (o *fakeObjects) PresignParts(_ context.Context, _, _ string, nums []int, _ time.Duration) ([]media.PartURL, error) {
	return nil, nil
}
func (o *fakeObjects) Complete(_ context.Context, _, _ string, _ []media.PartETag) error { return nil }
func (o *fakeObjects) Stat(_ context.Context, _ string) (int64, error)                   { return o.statSize, nil }
func (o *fakeObjects) Hash(_ context.Context, _ string) ([]byte, error)                  { return nil, nil }
func (o *fakeObjects) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "get://" + key, nil
}
func (o *fakeObjects) Abort(_ context.Context, key, _ string) error {
	o.aborted = append(o.aborted, key)
	return nil
}
func (o *fakeObjects) Remove(_ context.Context, key string) error {
	o.removed = append(o.removed, key)
	return nil
}

type row struct {
	b        Backup
	complete bool
}
type fakeStore struct{ m map[string]*row }

func newFakeStore() *fakeStore { return &fakeStore{m: map[string]*row{}} }
func (s *fakeStore) CreatePending(_ context.Context, b Backup) error {
	s.m[b.ID] = &row{b: b}
	return nil
}
func (s *fakeStore) Get(_ context.Context, id string) (Backup, error) {
	r, ok := s.m[id]
	if !ok {
		return Backup{}, ErrNotFound
	}
	return r.b, nil
}
func (s *fakeStore) MarkComplete(_ context.Context, id string) error {
	r, ok := s.m[id]
	if !ok {
		return ErrNotFound
	}
	r.complete = true
	return nil
}
func (s *fakeStore) Latest(_ context.Context, userID string) (Backup, error) {
	var latest *Backup
	for _, r := range s.m {
		if r.complete && r.b.UserID == userID {
			if latest == nil || r.b.CreatedAt.After(latest.CreatedAt) {
				b := r.b
				latest = &b
			}
		}
	}
	if latest == nil {
		return Backup{}, ErrNotFound
	}
	return *latest, nil
}
func (s *fakeStore) OldComplete(_ context.Context, userID, exceptID string) ([]Backup, error) {
	var out []Backup
	for id, r := range s.m {
		if r.complete && r.b.UserID == userID && id != exceptID {
			out = append(out, r.b)
		}
	}
	return out, nil
}
func (s *fakeStore) Delete(_ context.Context, id string) error { delete(s.m, id); return nil }

type harness struct {
	svc   *Service
	obj   *fakeObjects
	store *fakeStore
	seq   int
}

func newHarness() *harness {
	h := &harness{obj: &fakeObjects{}, store: newFakeStore()}
	h.svc = NewService(h.obj, h.store, 1<<20) // 1 MiB cap for tests
	h.svc.now = func() time.Time { return time.Unix(1_800_000_000, 0).Add(time.Duration(h.seq) * time.Second) }
	h.svc.newID = func() string { h.seq++; return "bk" + string(rune('0'+h.seq)) }
	return h
}

func who(u string) auth.Identity { return auth.Identity{UserID: u, DeviceID: "d1", SessionID: "s"} }

func code(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

// ── tests ────────────────────────────────────────────────────────────────

func TestCreate_PresignsAndRecordsPending(t *testing.T) {
	h := newHarness()
	res, err := h.svc.Create(context.Background(), who("u1"), 100_000)
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupID == "" || len(res.PartURLs) != 1 || res.PartSize != domain.PartSize {
		t.Fatalf("bad create result: %+v", res)
	}
	if _, ok := h.store.m[res.BackupID]; !ok {
		t.Fatal("pending backup not recorded")
	}
}

func TestCreate_SizeValidation(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Create(context.Background(), who("u1"), 0); code(t, err) != "VALIDATION_SIZE" {
		t.Fatal("zero size should be VALIDATION_SIZE")
	}
	if _, err := h.svc.Create(context.Background(), who("u1"), (1<<20)+1); code(t, err) != "VALIDATION_TOO_LARGE" {
		t.Fatal("over-cap should be VALIDATION_TOO_LARGE")
	}
}

func TestComplete_VerifiesSizeAndMarksComplete(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("u1"), 100_000)
	h.obj.statSize = 100_000 // matches

	if _, err := h.svc.Complete(context.Background(), who("u1"), res.BackupID, nil); err != nil {
		t.Fatal(err)
	}
	if !h.store.m[res.BackupID].complete {
		t.Fatal("backup should be marked complete")
	}
	// It's now restorable.
	if _, err := h.svc.Latest(context.Background(), who("u1")); err != nil {
		t.Fatalf("latest after complete: %v", err)
	}
}

func TestComplete_SizeMismatchRejectsAndCleansUp(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("u1"), 100_000)
	h.obj.statSize = 999 // mismatch

	if _, err := h.svc.Complete(context.Background(), who("u1"), res.BackupID, nil); code(t, err) != "VALIDATION_SIZE_MISMATCH" {
		t.Fatal("size mismatch should reject")
	}
	if _, ok := h.store.m[res.BackupID]; ok {
		t.Fatal("mismatched backup row should be deleted")
	}
	if len(h.obj.removed) != 1 {
		t.Fatal("mismatched blob should be removed")
	}
}

func TestComplete_OwnershipAndNotFound(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("u1"), 100_000)
	h.obj.statSize = 100_000

	if _, err := h.svc.Complete(context.Background(), who("mallory"), res.BackupID, nil); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("non-owner complete should be STATE_FORBIDDEN")
	}
	if _, err := h.svc.Complete(context.Background(), who("u1"), "nope", nil); code(t, err) != "BACKUP_NOT_FOUND" {
		t.Fatal("unknown backup should be BACKUP_NOT_FOUND")
	}
}

func TestComplete_NewBackupReplacesOld(t *testing.T) {
	h := newHarness()
	h.obj.statSize = 100_000

	first, _ := h.svc.Create(context.Background(), who("u1"), 100_000)
	_, _ = h.svc.Complete(context.Background(), who("u1"), first.BackupID, nil)

	second, _ := h.svc.Create(context.Background(), who("u1"), 100_000)
	if _, err := h.svc.Complete(context.Background(), who("u1"), second.BackupID, nil); err != nil {
		t.Fatal(err)
	}

	// The old backup's blob + row are reclaimed (1-active quota).
	if _, ok := h.store.m[first.BackupID]; ok {
		t.Fatal("old backup row should be deleted")
	}
	if len(h.obj.removed) != 1 || h.obj.removed[0] != first.ObjectKey {
		t.Fatalf("old blob should be removed, got %v", h.obj.removed)
	}
	// Latest is the new one.
	latest, _ := h.svc.Latest(context.Background(), who("u1"))
	if latest.BackupID != second.BackupID {
		t.Fatalf("latest = %s, want %s", latest.BackupID, second.BackupID)
	}
}

func TestLatest_NoBackup(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Latest(context.Background(), who("u1")); code(t, err) != "NO_BACKUP" {
		t.Fatal("no backup should be NO_BACKUP (404)")
	}
}
