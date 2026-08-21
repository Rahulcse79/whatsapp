package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/discovery/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

type fakeBackend struct {
	results []Result
	gotKind []domain.Kind
}

func (b *fakeBackend) Search(_ context.Context, _ string, kinds []domain.Kind, _ int) ([]Result, error) {
	b.gotKind = kinds
	return b.results, nil
}

type fakeSource struct{ docs []Doc }

func (s *fakeSource) AllDocs(_ context.Context) ([]Doc, error) { return s.docs, nil }

type fakeIndexer struct{ indexed []Doc }

func (i *fakeIndexer) Index(_ context.Context, d Doc) error {
	i.indexed = append(i.indexed, d)
	return nil
}
func (i *fakeIndexer) Delete(_ context.Context, _ domain.Kind, _ string) error { return nil }

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

func TestSearchRanksAndFiltersNonMatches(t *testing.T) {
	be := &fakeBackend{results: []Result{
		{Kind: domain.KindChannel, ID: "1", Title: "developers", Handle: "@devs"},
		{Kind: domain.KindUser, ID: "2", Title: "dev", Handle: "@dev"}, // exact
		{Kind: domain.KindCommunity, ID: "3", Title: "sports fans"},    // no match → dropped
		{Kind: domain.KindChannel, ID: "4", Title: "Cool Dev Club", Verified: true},
	}}
	svc := NewService(be, &fakeSource{}, &fakeIndexer{})

	out, err := svc.Search(context.Background(), who("u"), "dev", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(out))
	for i, r := range out {
		ids[i] = r.ID
	}
	// exact "dev" first, then prefix "developers", then word-match "Cool Dev Club";
	// "sports fans" (no match) is dropped.
	if len(out) != 3 || ids[0] != "2" || ids[1] != "1" || ids[2] != "4" {
		t.Fatalf("ranking wrong: %+v", ids)
	}
}

func TestSearchValidatesAndLimits(t *testing.T) {
	svc := NewService(&fakeBackend{}, &fakeSource{}, &fakeIndexer{})
	if _, err := svc.Search(context.Background(), who("u"), "x", nil, 10); codeOf(t, err) != "VALIDATION_QUERY" {
		t.Fatal("short query should 400")
	}
}

func TestSearchPassesKindFilter(t *testing.T) {
	be := &fakeBackend{}
	svc := NewService(be, &fakeSource{}, &fakeIndexer{})
	_, _ = svc.Search(context.Background(), who("u"), "news", []domain.Kind{domain.KindChannel}, 10)
	if len(be.gotKind) != 1 || be.gotKind[0] != domain.KindChannel {
		t.Fatalf("kind filter not passed: %+v", be.gotKind)
	}
}

func TestReindexPushesEveryDoc(t *testing.T) {
	src := &fakeSource{docs: []Doc{
		{Kind: domain.KindChannel, ID: "c1", Title: "News"},
		{Kind: domain.KindUser, ID: "u1", Title: "alice"},
	}}
	idx := &fakeIndexer{}
	svc := NewService(&fakeBackend{}, src, idx)
	n, err := svc.Reindex(context.Background())
	if err != nil || n != 2 || len(idx.indexed) != 2 {
		t.Fatalf("reindex: n=%d err=%v indexed=%d", n, err, len(idx.indexed))
	}
}
