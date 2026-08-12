package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Exercises the Tenor adapter against an in-process server: it must send the
// server's key + a well-formed query, normalize the response, and — the point of
// FR-MED-05 — forward NO client-identifying headers to the provider.
func TestTenorProvider_NormalizesAndForwardsNoClientIP(t *testing.T) {
	var gotQuery url.Values
	var gotHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotHeaders = r.Header.Clone()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{
					"id": "abc",
					"media_formats": map[string]any{
						"gif":     map[string]any{"url": "https://cdn/gif.gif", "dims": []int{200, 150}},
						"tinygif": map[string]any{"url": "https://cdn/tiny.gif", "dims": []int{90, 67}},
					},
				},
				{ // a result missing the full gif format is skipped
					"id":            "no-gif",
					"media_formats": map[string]any{"tinygif": map[string]any{"url": "https://cdn/only-tiny.gif"}},
				},
			},
		})
	}))
	defer srv.Close()

	p := NewTenorProviderWithBase("SECRETKEY", srv.URL)
	res, err := p.Search(context.Background(), "cat", 12)
	if err != nil {
		t.Fatal(err)
	}

	if len(res) != 1 {
		t.Fatalf("results = %d, want 1 (the gif-less hit is skipped)", len(res))
	}
	got := res[0]
	if got.ID != "abc" || got.URL != "https://cdn/gif.gif" || got.PreviewURL != "https://cdn/tiny.gif" {
		t.Fatalf("bad normalization: %+v", got)
	}
	if got.Width != 200 || got.Height != 150 {
		t.Fatalf("dims = %dx%d, want 200x150", got.Width, got.Height)
	}

	if gotQuery.Get("q") != "cat" || gotQuery.Get("key") != "SECRETKEY" || gotQuery.Get("limit") != "12" {
		t.Fatalf("query params wrong: %v", gotQuery)
	}
	if gotQuery.Get("client_key") != "whatsapp-v2" {
		t.Fatalf("client_key = %q, want whatsapp-v2", gotQuery.Get("client_key"))
	}

	// FR-MED-05: the provider must never receive a client-identifying header.
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded", "X-Client-Ip", "Via"} {
		if v := gotHeaders.Get(h); v != "" {
			t.Fatalf("provider received client header %s=%q — must be hidden", h, v)
		}
	}
}
