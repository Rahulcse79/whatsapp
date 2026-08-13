# Runbook — Push provider breaker open

**Alert:** `PushBreakerOpen` · **Severity:** ticket (one provider) / **page** (both providers) · **Fires:** a provider circuit breaker open > 5 m.

## What it means
`notification-svc`'s per-provider circuit breaker tripped (5 failures / 30 s →
open 60 s, half-open probe). A push provider (FCM / APNs / ntfy / WebPush) is
failing. One provider open → degraded (ticket); **both** open → users on the
locked screen aren't woken → page.

## Impact
Notifications for the affected provider aren't dispatched. **No data loss** — the
message still waits in the PG inbox and delivers on next app open/reconnect. The
cost is "phone didn't buzz."

## Diagnose
- Which provider? `sum by (provider) (push_breaker_state)` / the alert label.
- Provider-side outage vs our creds: check the provider status page; look for
  auth errors (expired FCM OAuth / APNs key) vs 5xx in `notification-svc` logs.
- Is the DLQ filling as a result? See [nats-dlq](nats-dlq.md) (`push.dlq`).

## Mitigate
1. **Provider outage**: nothing to do but wait — the breaker half-open probe
   auto-recovers when the provider returns; retries drain the queue. Confirm the
   alternate provider (if the user has one registered) is carrying the load.
2. **Our credentials**: rotate/fix the provider secret (FCM OAuth token source,
   APNs key/cert) and restart `notification-svc`; the breaker closes on the next
   probe.
3. Offline profile uses ntfy/WebPush only — verify the self-hosted ntfy is up.

## Verify recovery
Breaker closed (`push_breaker_state == closed`); `push.dlq` stops growing;
dispatch success rate recovers.

## Escalate
**Both** providers open (real page) → on-call lead; treat as user-impacting.

## Related
Alert `ops/alerts/platform.yaml` · notify (T0.16) · notification-svc-lld.md ·
[nats-dlq](nats-dlq.md).
