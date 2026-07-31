// Package notify is the push-dispatch pipeline: it consumes push.dispatch,
// resolves a device's token, and sends a WAKE SIGNAL (never plaintext) via the
// right provider, guarded by a per-provider circuit breaker. Payloads carry no
// content — the woken device fetches ciphertext and renders locally
// (FR-NOTIF-01). Design: Docs/05-services/notification-svc-lld.md.
package notify

import (
	"context"
	"errors"
)

// Provider mirrors push_tokens.provider (migration 000007).
type Provider int16

const (
	ProviderFCM      Provider = 0
	ProviderAPNs     Provider = 1
	ProviderAPNsVoIP Provider = 2
	ProviderNtfy     Provider = 3
	ProviderWebPush  Provider = 4
)

func (p Provider) String() string {
	switch p {
	case ProviderFCM:
		return "fcm"
	case ProviderAPNs:
		return "apns"
	case ProviderAPNsVoIP:
		return "apns_voip"
	case ProviderNtfy:
		return "ntfy"
	case ProviderWebPush:
		return "webpush"
	default:
		return "unknown"
	}
}

// Kind is the wake reason (events.v1.PushDispatch.kind).
type Kind int16

const (
	KindMessage Kind = 1
	KindCall    Kind = 2
	KindGeneric Kind = 3
)

// Payload is a wake signal — collapse metadata only, never message content.
type Payload struct {
	Kind        Kind
	CollapseKey string // repeated wakes for the same key coalesce at the provider
	RingID      string // set for KindCall (CallKit/ConnectionService)
}

// Dispatch is one unit of work from the push.dispatch stream.
type Dispatch struct {
	RecipientDeviceID string
	Payload           Payload
}

// ErrTokenInvalid is returned by a driver when the provider reports the token
// is dead (unregistered / 410). The pipeline deletes the token and acks —
// retrying is pointless and it must not count as a provider outage.
var ErrTokenInvalid = errors.New("notify: push token invalid")

// ErrNoToken is returned by the TokenStore when a device has no push token
// (e.g. web-only, or logged out). The pipeline acks: nothing to deliver.
var ErrNoToken = errors.New("notify: no push token")

// PushDriver delivers a wake signal to one device via one provider.
type PushDriver interface {
	Send(ctx context.Context, token string, p Payload) error
	Provider() Provider
}

// TokenStore resolves and prunes device push tokens (push_tokens table).
type TokenStore interface {
	Resolve(ctx context.Context, deviceID string) (token string, provider Provider, err error)
	Delete(ctx context.Context, deviceID string) error
}

// DLQPublisher parks a dispatch that exhausted its redeliveries on push.dlq
// for a human runbook (never silently dropped).
type DLQPublisher interface {
	PublishDLQ(ctx context.Context, d Dispatch) error
}
