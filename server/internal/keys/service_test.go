package keys

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
)

type fakeRepo struct {
	otps    map[string][]OneTimePrekey
	bundles []DeviceBundle
	noDev   bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{otps: map[string][]OneTimePrekey{}}
}

func (f *fakeRepo) ReplaceSignedPrekey(_ context.Context, _ string, _ SignedPrekey) error {
	return nil
}
func (f *fakeRepo) AddOneTimePrekeys(_ context.Context, d string, o []OneTimePrekey) error {
	f.otps[d] = append(f.otps[d], o...)
	return nil
}
func (f *fakeRepo) CountAvailable(_ context.Context, d string) (int, error) {
	return len(f.otps[d]), nil
}
func (f *fakeRepo) ConsumeBundle(_ context.Context, _ string) ([]DeviceBundle, error) {
	if f.noDev {
		return nil, ErrNoDevices
	}
	return f.bundles, nil
}

type memLimiter struct{ m *ratelimit.MemoryLimiter }

func (l memLimiter) Allow(_ context.Context, k string, p ratelimit.Params) (ratelimit.Result, error) {
	return l.m.Allow(k, p)
}

type denyLimiter struct{}

func (denyLimiter) Allow(_ context.Context, _ string, _ ratelimit.Params) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: false, RetryAfter: 7 * time.Second}, nil
}

func newSvc() (*Service, *fakeRepo) {
	r := newFakeRepo()
	return NewService(r, memLimiter{ratelimit.NewMemoryLimiter()}), r
}

func apiCode(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func TestPublish_Validation(t *testing.T) {
	s, _ := newSvc()
	ctx := context.Background()

	if _, err := s.Publish(ctx, "d1", SignedPrekey{}, nil); apiCode(t, err) != "VALIDATION_PREKEYS" {
		t.Fatal("empty signed prekey accepted")
	}
	good := SignedPrekey{KeyID: 1, Pubkey: []byte("pk"), Signature: []byte("sig")}
	tooMany := make([]OneTimePrekey, MaxOneTimePrekeysPerUpload+1)
	if _, err := s.Publish(ctx, "d1", good, tooMany); apiCode(t, err) != "VALIDATION_PREKEYS" {
		t.Fatal("over-limit prekey batch accepted")
	}
	if _, err := s.Publish(ctx, "d1", good, []OneTimePrekey{{KeyID: 1}}); apiCode(t, err) != "VALIDATION_PREKEYS" {
		t.Fatal("prekey without pubkey accepted")
	}
}

func TestPublish_LowWaterFlag(t *testing.T) {
	s, _ := newSvc()
	good := SignedPrekey{KeyID: 1, Pubkey: []byte("pk"), Signature: []byte("sig")}

	// Below the low-water mark → flagged.
	few := make([]OneTimePrekey, 5)
	for i := range few {
		few[i] = OneTimePrekey{KeyID: int32(i), Pubkey: []byte("k")}
	}
	res, err := s.Publish(context.Background(), "d1", good, few)
	if err != nil {
		t.Fatal(err)
	}
	if res.Available != 5 || !res.LowWater {
		t.Fatalf("want available=5 low_water=true, got %+v", res)
	}
}

func TestFetchBundle_RateLimited(t *testing.T) {
	r := newFakeRepo()
	s := NewService(r, denyLimiter{})
	_, err := s.FetchBundle(context.Background(), "req", "target")
	if apiCode(t, err) != "RATE_LIMITED" {
		t.Fatalf("want RATE_LIMITED, got %v", err)
	}
}

func TestFetchBundle_NoDevices(t *testing.T) {
	s, r := newSvc()
	r.noDev = true
	if _, err := s.FetchBundle(context.Background(), "req", "ghost"); apiCode(t, err) != "USER_NOT_FOUND" {
		t.Fatalf("want USER_NOT_FOUND, got %v", err)
	}
}

func TestFetchBundle_OK(t *testing.T) {
	s, r := newSvc()
	r.bundles = []DeviceBundle{{DeviceID: "d1", IdentityKey: []byte("ik"),
		SignedPrekey: SignedPrekey{KeyID: 3, Pubkey: []byte("sp")}}}
	got, err := s.FetchBundle(context.Background(), "req", "u1")
	if err != nil || len(got) != 1 || got[0].DeviceID != "d1" {
		t.Fatalf("unexpected: %+v err=%v", got, err)
	}
}
