package adapters

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

// HTTPDispatcher delivers a bot event to its webhook: an HTTPS POST carrying the
// JSON payload and an X-WA-Signature header (hex HMAC-SHA256 under the bot's
// secret) so the bot can authenticate it. Best-effort with a short timeout.
//
// SEAM/hardening: a production deployment must run this behind egress controls
// that block private/link-local ranges (SSRF) — the https-only validation is the
// baseline, not the full mitigation.
type HTTPDispatcher struct{ client *http.Client }

func NewHTTPDispatcher() *HTTPDispatcher {
	return &HTTPDispatcher{client: &http.Client{Timeout: 5 * time.Second}}
}

func (d *HTTPDispatcher) Deliver(ctx context.Context, url, signature string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WA-Signature", signature)
	req.Header.Set("User-Agent", "WhatsAppV2-Bot-Webhook/1")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bots: webhook returned %d", resp.StatusCode)
	}
	return nil
}

// NoopDispatcher swallows deliveries — the default in dev, where no live bot
// endpoint is reachable.
type NoopDispatcher struct{}

func (NoopDispatcher) Deliver(context.Context, string, string, []byte) error { return nil }
