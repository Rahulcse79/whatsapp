package media

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeGifProvider struct {
	results   []GifResult
	err       error
	calls     int
	lastQuery string
	lastLimit int
}

func (p *fakeGifProvider) Search(_ context.Context, query string, limit int) ([]GifResult, error) {
	p.calls++
	p.lastQuery = query
	p.lastLimit = limit
	return p.results, p.err
}

func TestGif_DisabledWithoutProvider(t *testing.T) {
	g := NewGifService(nil, fakeRate{allow: true})
	if g.Enabled() {
		t.Fatal("service with no provider must report disabled")
	}
	_, err := g.Search(context.Background(), ident("u1"), "cat", 0)
	if got := code(t, err); got != "FEATURE_DISABLED" {
		t.Fatalf("code = %q, want FEATURE_DISABLED", got)
	}
}

func TestGif_ValidatesQuery(t *testing.T) {
	p := &fakeGifProvider{}
	g := NewGifService(p, fakeRate{allow: true})

	if got := code(t, mustErr(g.Search(context.Background(), ident("u1"), "   ", 0))); got != "VALIDATION_QUERY" {
		t.Fatalf("empty query code = %q, want VALIDATION_QUERY", got)
	}
	long := strings.Repeat("x", gifMaxQueryRunes+1)
	if got := code(t, mustErr(g.Search(context.Background(), ident("u1"), long, 0))); got != "VALIDATION_QUERY" {
		t.Fatalf("long query code = %q, want VALIDATION_QUERY", got)
	}
	if p.calls != 0 {
		t.Fatalf("provider must not be called for an invalid query (calls=%d)", p.calls)
	}
}

func TestGif_RateLimitedBeforeProvider(t *testing.T) {
	p := &fakeGifProvider{}
	g := NewGifService(p, fakeRate{allow: false})
	if got := code(t, mustErr(g.Search(context.Background(), ident("u1"), "cat", 0))); got != "RATE_LIMITED" {
		t.Fatalf("code = %q, want RATE_LIMITED", got)
	}
	if p.calls != 0 {
		t.Fatal("provider must not be hit once rate-limited")
	}
}

func TestGif_HappyTrimsAndDefaultsLimit(t *testing.T) {
	p := &fakeGifProvider{results: []GifResult{{ID: "1", URL: "u", PreviewURL: "p", Width: 10, Height: 20}}}
	g := NewGifService(p, fakeRate{allow: true})
	res, err := g.Search(context.Background(), ident("u1"), "  cat  ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != "1" {
		t.Fatalf("bad results: %+v", res)
	}
	if p.lastQuery != "cat" {
		t.Fatalf("query = %q, want trimmed 'cat'", p.lastQuery)
	}
	if p.lastLimit != gifDefaultLimit {
		t.Fatalf("limit = %d, want default %d", p.lastLimit, gifDefaultLimit)
	}
}

func TestGif_LimitClamped(t *testing.T) {
	p := &fakeGifProvider{}
	g := NewGifService(p, fakeRate{allow: true})

	_, _ = g.Search(context.Background(), ident("u1"), "cat", gifMaxLimit+100)
	if p.lastLimit != gifDefaultLimit {
		t.Fatalf("over-cap limit = %d, want default %d", p.lastLimit, gifDefaultLimit)
	}
	_, _ = g.Search(context.Background(), ident("u1"), "cat", 10)
	if p.lastLimit != 10 {
		t.Fatalf("in-range limit = %d, want 10", p.lastLimit)
	}
}

func TestGif_ProviderErrorIsBadGateway(t *testing.T) {
	p := &fakeGifProvider{err: errors.New("upstream boom")}
	g := NewGifService(p, fakeRate{allow: true})
	if got := code(t, mustErr(g.Search(context.Background(), ident("u1"), "cat", 0))); got != "PROVIDER_UNAVAILABLE" {
		t.Fatalf("code = %q, want PROVIDER_UNAVAILABLE", got)
	}
}

func mustErr(_ []GifResult, err error) error { return err }
