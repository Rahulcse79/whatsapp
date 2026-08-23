package adapters

import (
	"context"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/media/domain"
)

func TestNewCDNDeliveryRequiresBothHalves(t *testing.T) {
	cases := []struct{ base, key string }{
		{"", "secret"},
		{"https://cdn.test", ""},
		{"  ", "  "},
	}
	for _, c := range cases {
		if _, err := NewCDNDelivery(c.base, c.key); err != ErrCDNNotConfigured {
			t.Errorf("base=%q key=%q: want ErrCDNNotConfigured, got %v", c.base, c.key, err)
		}
	}
	if _, err := NewCDNDelivery("not-a-url", "secret"); err == nil {
		t.Error("a malformed base URL must fail at construction, not per request")
	}
}

func TestCDNDeliveryMintsVerifiableURL(t *testing.T) {
	const secret = "edge-key"
	d, err := NewCDNDelivery("https://cdn.example.test/media", secret)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Cacheable() {
		t.Error("CDN delivery should report itself cacheable")
	}

	raw, err := d.DownloadURL(context.Background(), "media/abc.bin", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "cdn.example.test" {
		t.Fatalf("URL should point at the edge, got %s", u.Host)
	}

	// The edge must be able to validate exactly what we minted.
	exp, err := strconv.ParseInt(u.Query().Get(domain.ExpiryParam), 10, 64)
	if err != nil {
		t.Fatalf("expiry param unparseable: %v", err)
	}
	if err := domain.VerifyCDNToken([]byte(secret), "media/abc.bin", exp, time.Now().Unix(), u.Query().Get(domain.SigParam)); err != nil {
		t.Fatalf("edge verification of a freshly minted URL failed: %v", err)
	}
	// And the expiry must actually be in the future by roughly the TTL asked for.
	if d := time.Until(time.Unix(exp, 0)); d < 14*time.Minute || d > 16*time.Minute {
		t.Fatalf("expiry should track the requested TTL, got %v", d)
	}
}
