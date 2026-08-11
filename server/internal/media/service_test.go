package media

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/media/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeStore struct {
	byID  map[string]Object
	byKey map[string]string // objectKey → id
}

func newFakeStore() *fakeStore {
	return &fakeStore{byID: map[string]Object{}, byKey: map[string]string{}}
}

func (s *fakeStore) CreatePending(_ context.Context, o Object) error {
	s.byID[o.ID] = o
	s.byKey[o.ObjectKey] = o.ID
	return nil
}
func (s *fakeStore) Get(_ context.Context, id string) (Object, error) {
	o, ok := s.byID[id]
	if !ok {
		return Object{}, ErrNotFound
	}
	return o, nil
}
func (s *fakeStore) GetByKey(_ context.Context, key string) (Object, error) {
	id, ok := s.byKey[key]
	if !ok {
		return Object{}, ErrNotFound
	}
	return s.byID[id], nil
}
func (s *fakeStore) MarkComplete(_ context.Context, id string, expiresAt time.Time) error {
	o, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	o.State = domain.StateComplete
	o.ExpiresAt = &expiresAt
	s.byID[id] = o
	return nil
}
func (s *fakeStore) AddRef(_ context.Context, key string) (int32, error) {
	id, ok := s.byKey[key]
	if !ok {
		return 0, ErrNotFound
	}
	o := s.byID[id]
	o.Refcount++
	s.byID[id] = o
	return o.Refcount, nil
}
func (s *fakeStore) DecRef(_ context.Context, key string) (int32, error) {
	id, ok := s.byKey[key]
	if !ok {
		return 0, ErrNotFound
	}
	o := s.byID[id]
	if o.Refcount > 0 {
		o.Refcount--
	}
	s.byID[id] = o
	return o.Refcount, nil
}
func (s *fakeStore) SweepCandidates(_ context.Context, _ time.Time, _ int) ([]Object, error) {
	var out []Object
	for _, o := range s.byID {
		if o.Refcount == 0 {
			out = append(out, o)
		}
	}
	return out, nil
}
func (s *fakeStore) Delete(_ context.Context, id string) error {
	if o, ok := s.byID[id]; ok {
		delete(s.byKey, o.ObjectKey)
		delete(s.byID, id)
	}
	return nil
}

type fakeObjects struct {
	statSize    int64
	hash        []byte
	completeErr error
	removed     []string
	aborted     []string
}

func (o *fakeObjects) StartUpload(_ context.Context, key string, parts int, _ time.Duration) (string, []PartURL, error) {
	urls := make([]PartURL, parts)
	for i := 0; i < parts; i++ {
		urls[i] = PartURL{PartNumber: i + 1, URL: "https://minio/put/" + key + "?part=" + string(rune('1'+i))}
	}
	return "handle-" + key, urls, nil
}
func (o *fakeObjects) PresignParts(_ context.Context, key, _ string, nums []int, _ time.Duration) ([]PartURL, error) {
	urls := make([]PartURL, 0, len(nums))
	for _, n := range nums {
		urls = append(urls, PartURL{PartNumber: n, URL: "https://minio/put/" + key})
	}
	return urls, nil
}
func (o *fakeObjects) Complete(_ context.Context, _, _ string, _ []PartETag) error {
	return o.completeErr
}
func (o *fakeObjects) Stat(_ context.Context, _ string) (int64, error)  { return o.statSize, nil }
func (o *fakeObjects) Hash(_ context.Context, _ string) ([]byte, error) { return o.hash, nil }
func (o *fakeObjects) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://minio/get/" + key, nil
}
func (o *fakeObjects) Abort(_ context.Context, key, _ string) error {
	o.aborted = append(o.aborted, key)
	return nil
}
func (o *fakeObjects) Remove(_ context.Context, key string) error {
	o.removed = append(o.removed, key)
	return nil
}

type fakeSessions struct{ m map[string]UploadSession }

func newFakeSessions() *fakeSessions { return &fakeSessions{m: map[string]UploadSession{}} }
func (s *fakeSessions) Save(_ context.Context, sess UploadSession, _ time.Duration) error {
	s.m[sess.UploadID] = sess
	return nil
}
func (s *fakeSessions) Load(_ context.Context, id string) (UploadSession, error) {
	sess, ok := s.m[id]
	if !ok {
		return UploadSession{}, ErrNotFound
	}
	return sess, nil
}
func (s *fakeSessions) Delete(_ context.Context, id string) error { delete(s.m, id); return nil }

type fakeQuota struct {
	allowed bool
	err     error
}

func (q fakeQuota) CheckAndReserve(_ context.Context, _ string, _ int64) (bool, int64, error) {
	return q.allowed, 0, q.err
}

type fakeRate struct {
	allow bool
	err   error
}

func (r fakeRate) Allow(_ context.Context, _ string) (bool, error) { return r.allow, r.err }

type fakeEvents struct {
	uploaded, orphaned   []string
	refAdded, refRemoved int
}

func (e *fakeEvents) Uploaded(_ context.Context, key string)          { e.uploaded = append(e.uploaded, key) }
func (e *fakeEvents) RefAdded(_ context.Context, _ string, _ int32)   { e.refAdded++ }
func (e *fakeEvents) RefRemoved(_ context.Context, _ string, _ int32) { e.refRemoved++ }
func (e *fakeEvents) Orphaned(_ context.Context, key string)          { e.orphaned = append(e.orphaned, key) }

type harness struct {
	svc      *Service
	store    *fakeStore
	objects  *fakeObjects
	sessions *fakeSessions
	events   *fakeEvents
}

func newHarness() *harness {
	store, objects, sessions, events := newFakeStore(), &fakeObjects{}, newFakeSessions(), &fakeEvents{}
	svc := NewService(store, objects, sessions, fakeQuota{allowed: true}, fakeRate{allow: true}, events)
	return &harness{svc: svc, store: store, objects: objects, sessions: sessions, events: events}
}

func ident(u string) auth.Identity { return auth.Identity{UserID: u, DeviceID: "d1", SessionID: "s1"} }

func code(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

// ── tests ────────────────────────────────────────────────────────────────

func TestCreateUpload_Happy(t *testing.T) {
	h := newHarness()
	res, err := h.svc.CreateUpload(context.Background(), ident("u1"), 1000, []byte("hash"), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if res.UploadID == "" || len(res.PartURLs) != 1 || res.PartSize != domain.PartSize {
		t.Fatalf("bad create result: %+v", res)
	}
	if _, ok := h.store.byID[res.UploadID]; !ok {
		t.Fatal("pending row not created")
	}
	if _, ok := h.sessions.m[res.UploadID]; !ok {
		t.Fatal("upload session not saved")
	}
}

func TestCreateUpload_Rejections(t *testing.T) {
	// too large
	h := newHarness()
	if _, err := h.svc.CreateUpload(context.Background(), ident("u1"), domain.MaxFileSize+1, []byte("h"), ""); code(t, err) != "VALIDATION_TOO_LARGE" {
		t.Fatal("over-cap accepted")
	}
	// rate limited
	h = newHarness()
	h.svc.rate = fakeRate{allow: false}
	if _, err := h.svc.CreateUpload(context.Background(), ident("u1"), 100, []byte("h"), ""); code(t, err) != "RATE_LIMITED" {
		t.Fatal("rate limit not enforced")
	}
	// quota denied
	h = newHarness()
	h.svc.quota = fakeQuota{allowed: false}
	if _, err := h.svc.CreateUpload(context.Background(), ident("u1"), 100, []byte("h"), ""); code(t, err) != "STATE_QUOTA_EXCEEDED" {
		t.Fatal("quota not enforced")
	}
	// quota unavailable → fail closed
	h = newHarness()
	h.svc.quota = fakeQuota{err: errors.New("grpc down")}
	if _, err := h.svc.CreateUpload(context.Background(), ident("u1"), 100, []byte("h"), ""); code(t, err) != "TRANSIENT_UNAVAILABLE" {
		t.Fatal("quota outage did not fail closed")
	}
}

func TestCompleteUpload_HappyAndVerify(t *testing.T) {
	h := newHarness()
	h.objects.statSize = 1000
	h.objects.hash = []byte("hash")
	res, _ := h.svc.CreateUpload(context.Background(), ident("u1"), 1000, []byte("hash"), "")

	mediaID, err := h.svc.CompleteUpload(context.Background(), ident("u1"), res.UploadID, []PartETag{{PartNumber: 1, ETag: "e"}})
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if obj := h.store.byID[mediaID]; obj.State != domain.StateComplete {
		t.Fatal("object not marked complete")
	}
	if len(h.events.uploaded) != 1 {
		t.Fatal("uploaded event not emitted")
	}
	if _, ok := h.sessions.m[res.UploadID]; ok {
		t.Fatal("session not cleared after complete")
	}
}

func TestCompleteUpload_HashMismatchCleansUp(t *testing.T) {
	h := newHarness()
	h.objects.statSize = 1000
	h.objects.hash = []byte("WRONG")
	res, _ := h.svc.CreateUpload(context.Background(), ident("u1"), 1000, []byte("hash"), "")

	if _, err := h.svc.CompleteUpload(context.Background(), ident("u1"), res.UploadID, nil); code(t, err) != "VALIDATION_HASH_MISMATCH" {
		t.Fatal("hash mismatch not rejected")
	}
	if _, ok := h.store.byID[res.UploadID]; ok {
		t.Fatal("row not cleaned up on hash mismatch (no row committed)")
	}
	if len(h.objects.removed) != 1 {
		t.Fatal("blob not removed on mismatch")
	}
}

func TestCompleteUpload_SizeMismatch(t *testing.T) {
	h := newHarness()
	h.objects.statSize = 999 // != 1000
	res, _ := h.svc.CreateUpload(context.Background(), ident("u1"), 1000, []byte("hash"), "")
	if _, err := h.svc.CompleteUpload(context.Background(), ident("u1"), res.UploadID, nil); code(t, err) != "VALIDATION_SIZE_MISMATCH" {
		t.Fatal("size mismatch not rejected")
	}
}

func TestCompleteUpload_NotOwner(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.CreateUpload(context.Background(), ident("u1"), 1000, []byte("hash"), "")
	if _, err := h.svc.CompleteUpload(context.Background(), ident("intruder"), res.UploadID, nil); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("foreign upload completion allowed")
	}
}

func TestDownloadURLs_SkipsIncomplete(t *testing.T) {
	h := newHarness()
	h.objects.statSize, h.objects.hash = 1000, []byte("hash")
	res, _ := h.svc.CreateUpload(context.Background(), ident("u1"), 1000, []byte("hash"), "")
	completeKey := res.ObjectKey
	// a second, still-pending upload
	res2, _ := h.svc.CreateUpload(context.Background(), ident("u1"), 500, []byte("h2"), "")
	if _, err := h.svc.CompleteUpload(context.Background(), ident("u1"), res.UploadID, nil); err != nil {
		t.Fatal(err)
	}

	urls, err := h.svc.DownloadURLs(context.Background(), ident("u1"), []string{completeKey, res2.ObjectKey, "unknown/key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0].Key != completeKey {
		t.Fatalf("only the completed object should get a URL: %+v", urls)
	}
}

func TestRefcounting(t *testing.T) {
	h := newHarness()
	h.objects.statSize, h.objects.hash = 1000, []byte("hash")
	res, _ := h.svc.CreateUpload(context.Background(), ident("u1"), 1000, []byte("hash"), "")
	_, _ = h.svc.CompleteUpload(context.Background(), ident("u1"), res.UploadID, nil)

	if err := h.svc.AddRef(context.Background(), res.ObjectKey); err != nil {
		t.Fatal(err)
	}
	if got := h.store.byID[res.UploadID].Refcount; got != 1 {
		t.Fatalf("refcount = %d, want 1", got)
	}
	if err := h.svc.DecRef(context.Background(), res.ObjectKey); err != nil {
		t.Fatal(err)
	}
	if got := h.store.byID[res.UploadID].Refcount; got != 0 {
		t.Fatalf("refcount = %d, want 0", got)
	}
	if h.events.refAdded != 1 || h.events.refRemoved != 1 {
		t.Fatal("ref events not emitted")
	}
	// AddRef on unknown key → MEDIA_NOT_FOUND
	if err := h.svc.AddRef(context.Background(), "nope"); !bytes.Contains([]byte(code(t, err)), []byte("MEDIA_NOT_FOUND")) {
		t.Fatal("addref on unknown object should 404")
	}
}
