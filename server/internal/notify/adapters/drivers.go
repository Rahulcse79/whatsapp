package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/notify"
)

// httpClient is shared by the HTTP-based drivers with a tight timeout — a slow
// provider must trip the breaker, not pin a worker.
var httpClient = &http.Client{Timeout: 5 * time.Second}

// ── ntfy (UnifiedPush, offline profile) — real ────────────────────────────

// NtfyDriver POSTs a wake signal to a self-hosted ntfy endpoint. The token IS
// the endpoint URL (UnifiedPush). Body is a minimal, content-free ping — the
// device wakes, fetches ciphertext, and renders locally (FR-NOTIF-01).
type NtfyDriver struct{ client *http.Client }

func NewNtfyDriver() *NtfyDriver { return &NtfyDriver{client: httpClient} }

func (NtfyDriver) Provider() notify.Provider { return notify.ProviderNtfy }

func (d NtfyDriver) Send(ctx context.Context, endpoint string, _ notify.Payload) error {
	//nolint:gosec // G107: the endpoint is the device's registered push URL (trusted config), not user taint.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte("wake")))
	if err != nil {
		return fmt.Errorf("ntfy request: %w", err)
	}
	// No message body content; a data push only tells the device to sync.
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return notify.ErrTokenInvalid
	default:
		return fmt.Errorf("ntfy status %d", resp.StatusCode)
	}
}

// ── FCM (Android/web) — real (HTTP v1, data-only) ─────────────────────────

// TokenSource yields a short-lived OAuth2 access token for FCM. Injected so
// the driver stays dependency-free (the credential/OAuth wiring is the
// deployment's concern).
type TokenSource func(ctx context.Context) (string, error)

// FCMDriver sends data-only messages via the FCM HTTP v1 API. Data-only (no
// notification block) keeps content off the wire: the payload is a wake signal.
type FCMDriver struct {
	client    *http.Client
	projectID string
	token     TokenSource
}

func NewFCMDriver(projectID string, token TokenSource) *FCMDriver {
	return &FCMDriver{client: httpClient, projectID: projectID, token: token}
}

func (FCMDriver) Provider() notify.Provider { return notify.ProviderFCM }

func (d FCMDriver) Send(ctx context.Context, token string, p notify.Payload) error {
	if d.token == nil {
		return errors.New("fcm not configured: no OAuth token source")
	}
	access, err := d.token(ctx)
	if err != nil {
		return fmt.Errorf("fcm oauth: %w", err)
	}
	body, _ := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": token,
			// Data-only wake signal — never message content.
			"data": map[string]string{
				"kind":         fmt.Sprintf("%d", p.Kind),
				"collapse_key": p.CollapseKey,
				"ring_id":      p.RingID,
			},
			"android": map[string]any{"priority": priorityFor(p.Kind)},
		},
	})
	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", d.projectID)
	//nolint:gosec // G107: fixed FCM host, project id from trusted config.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("fcm request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("fcm send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized:
		// UNREGISTERED (404) — the app was uninstalled / token rotated.
		return notify.ErrTokenInvalid
	default:
		return fmt.Errorf("fcm status %d", resp.StatusCode)
	}
}

func priorityFor(k notify.Kind) string {
	if k == notify.KindCall {
		return "high" // lock-screen ring must wake immediately
	}
	return "normal"
}

// ── APNs / WebPush — stubs (real drivers need p8 JWT / VAPID + encryption) ──

// APNsDriver is a placeholder: APNs needs HTTP/2 with a p8 JWT (and PushKit for
// VoIP). It refuses loudly rather than silently dropping, so misconfiguration
// is obvious. iOS support is a documented follow-up (HLD §17.5 iOS limits).
type APNsDriver struct{ voip bool }

func NewAPNsDriver(voip bool) *APNsDriver { return &APNsDriver{voip: voip} }

func (d APNsDriver) Provider() notify.Provider {
	if d.voip {
		return notify.ProviderAPNsVoIP
	}
	return notify.ProviderAPNs
}

func (APNsDriver) Send(context.Context, string, notify.Payload) error {
	return errors.New("apns driver not configured (needs p8 JWT / PushKit)")
}

// WebPushDriver is a placeholder: Web Push needs VAPID signing and payload
// encryption (RFC 8291). Stub until the web client's push is wired.
type WebPushDriver struct{}

func NewWebPushDriver() *WebPushDriver { return &WebPushDriver{} }

func (WebPushDriver) Provider() notify.Provider { return notify.ProviderWebPush }

func (WebPushDriver) Send(context.Context, string, notify.Payload) error {
	return errors.New("webpush driver not configured (needs VAPID + RFC 8291 encryption)")
}

var (
	_ notify.PushDriver = (*NtfyDriver)(nil)
	_ notify.PushDriver = (*FCMDriver)(nil)
	_ notify.PushDriver = (*APNsDriver)(nil)
	_ notify.PushDriver = (*WebPushDriver)(nil)
)
