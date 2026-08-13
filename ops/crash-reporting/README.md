# Crash reporting (self-hosted, PII-scrubbed) — HLD §18.1

Client and server crashes report to a **self-hosted** GlitchTip (Sentry-API
compatible) instance. Nothing goes to a third-party SaaS, and **payloads are
scrubbed of PII before they leave the process** — the same privacy rule as the
rest of the product-analytics plane (T4.03): the server can't analyse content it
can't read, and we don't want it leaking through a stack trace either.

## Deployment

GlitchTip runs in-cluster alongside the LGTM observability stack. It is plain
infra (a Helm release / compose stack); no application change is needed to stand
it up. Point clients and servers at it with a project DSN:

| Env var | Consumer | Notes |
|---|---|---|
| `WA_SENTRY_DSN` | server deployables | unset → crash reporting disabled (`crash.NoopReporter`) |
| `WA_SENTRY_DSN_WEB` | web PWA build | injected at build; scrubbed client-side |
| `WA_SENTRY_DSN_MOBILE` | RN/Expo build | injected at build; scrubbed client-side |
| `WA_SENTRY_ENV` | all | `dev` / `staging` / `prod` (release health separation) |

Offline profile: leave the DSNs unset (or point at the offline GlitchTip); the
platform degrades to no crash export, never to an external host.

## PII scrubbing (the part that is code, not config)

Every payload passes a redaction step **before** transport, on both the client
SDK (`beforeSend`) and the server (`internal/platform/crash`):

- **Server:** `crash.ScrubbingReporter` wraps the transport. `Scrubber.Text`
  redacts JWTs, `Bearer` tokens, emails, UUIDs (user/device/session ids), IPv4
  addresses, and phone numbers from messages and stack frames; `Scrubber.Tags`
  drops sensitive keys (`authorization`, `cookie`, `password`, `phone`, `otp`,
  …) wholesale and text-scrubs the rest. Unit-tested in `crash/scrub_test.go`.
- **Clients:** the SDK `beforeSend` hook applies the equivalent redaction list
  before the event leaves the device. Breadcrumbs carrying message bodies are
  disabled (content is E2EE and must never appear in a crash report).

## Crash-free sessions metric

Clients periodically report session health (sessions started vs. sessions that
crashed). `crash.CrashFreeTracker` folds those counts into the crash-free ratio,
published as the Prometheus gauge **`product_crash_free_ratio`** and rendered on
the **Product (metadata-only)** Grafana dashboard next to DAU/MAU. With no data
the ratio reads 1.0, so a fresh deploy is not mistaken for an outage.
