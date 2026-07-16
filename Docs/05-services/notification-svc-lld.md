# LLD — notification-svc

| Doc | Push dispatch pipeline |
|---|---|
| Status | v1.0 · Go; stateless ×2; consumes `push.dispatch`; owns `push_tokens` |

## 1. Pipeline

```
NATS push.dispatch (durable pull consumer, batch ≤ 50)
  → resolve device → token+provider (LRU cache 60 s over push_tokens)
  → suppression checks: device has live route? muted chat? (collapse rules)
  → provider driver send (breaker-wrapped)
  → ok: ack NATS │ token-invalid: delete token, ack │ transient: nack (redeliver ≤ 5 → push.dlq)
```

## 2. Provider drivers (Strategy — the offline profile hinges on this interface)

```go
type PushDriver interface {
    Send(ctx, Token, Payload) error   // Payload NEVER contains plaintext content
    Kind() Provider                   // fcm | apns | apns_voip | ntfy | webpush
}
```

| Driver | Notes |
|---|---|
| FCM | HTTP v1, data-only messages, high-priority for calls (ConnectionService) |
| APNs | token-based auth (p8); background pushes for msgs; **PushKit VoIP for calls** (CallKit) |
| ntfy (UnifiedPush) | Offline profile (HLD §17.5); self-hosted; wake semantics = app persistent conn |
| WebPush | VAPID; PWA notifications |

Payload = wake signal only: `{kind: msg|call, collapse_key, ring_id?}` — the device fetches ciphertext via resume and renders locally (FR-NOTIF-01). This is load-bearing for privacy: **content never transits Google/Apple.**

## 3. Resilience

- **Circuit breaker per provider:** 5 failures/30 s → open 60 s; while open, messages nack → JetStream holds them (24 h retention) → burst-drained on close. Provider outage = delayed pushes, never lost messages (inbox is truth regardless).
- **Token lifecycle:** provider feedback (`Unregistered`/410) deletes token + marks device for re-registration hint on next connect; `failing_since` > 30 d → purge.
- **Rate shaping:** per-provider concurrency caps (FCM 100 inflight, APNs 50/conn with HTTP/2 multiplexing).

## 4. Call-push special path

`push.dispatch{kind: call}` bypasses batch queue → dedicated high-priority consumer (call ring latency budget: push handoff p95 ≤ 2 s). APNs VoIP + FCM high-priority; failure → SLI + immediate fallback WS ring continues regardless (parallel, not sequential).

## 5. Failure modes

| Failure | Behavior |
|---|---|
| Both providers down | Queue absorbs 24 h; users still get messages on app open (inbox) |
| Token DB unavailable | Cache serves 60 s; then nack (redelivery) |
| DLQ growth | Alert; runbook: inspect payload kinds, provider status pages |
