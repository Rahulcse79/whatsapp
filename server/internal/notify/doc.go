// Package notify owns push dispatch: the push.dispatch consumer, provider
// drivers behind a common interface (FCM, APNs, APNs-VoIP, ntfy, WebPush),
// circuit breakers, and push-token lifecycle. Payloads are wake signals
// only — plaintext content never transits a push provider (FR-NOTIF-01).
// Used by cmd/notification-svc.
//
// Design: Docs/05-services/notification-svc-lld.md.
package notify
