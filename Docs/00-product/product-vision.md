# Product Vision, Goals & Success Metrics

| Doc | Product Vision — WhatsApp V2 |
|---|---|
| Status | v1.0 |
| Upstream | [/HLD.md](../../HLD.md) §2 |

## 1. Vision

**A private-by-architecture messaging platform you can run yourself.** Every conversation, call, and story is end-to-end encrypted; the operator of the servers — including us — cannot read user content even under compromise or compulsion. Unlike WhatsApp, the entire platform is deployable on self-owned hardware, down to a single offline server (HLD §17.5).

One sentence: *WhatsApp's product experience, Signal's trust model, self-hostable like Matrix — without Matrix's complexity.*

## 2. Product pillars

1. **Privacy is structural, not policy.** The relay model means the server physically lacks the data that a breach, subpoena, or rogue admin would target.
2. **It just works offline-first.** Local-first clients: instant open, full history, search, and queued sends with zero connectivity; sync when back online.
3. **Calls that feel native.** Lock-screen ringing, <3 s setup, resilient on bad networks (Opus FEC, simulcast, TURN/TCP fallback).
4. **Small-team operable.** 5 deployables, boring technology, one observability pane. 2–4 engineers run production.

## 3. Goals (V2)

| # | Goal | Measure |
|---|---|---|
| G1 | Full messaging parity with WhatsApp core | Feature checklist in [functional-requirements.md](../01-requirements/functional-requirements.md) 100% shipped |
| G2 | Production-grade reliability at 20k concurrent | SLOs green 2 consecutive weeks under synthetic load (P4 exit) |
| G3 | E2EE for everything by default | Zero plaintext content paths server-side; external audit passes |
| G4 | Self-hostable, including air-gapped | Full stack boots on one box from `deploy/compose` |
| G5 | Scale without re-architecture | 100k/1M ladder requires only node counts + planned partitioning (HLD §16.3) |

## 4. Success metrics

| Metric | Target (6 months post-launch) |
|---|---|
| Peak concurrent users served | ≥ 20,000 with SLOs green |
| Message p95 end-to-end latency (online↔online) | ≤ 500 ms |
| Call setup p95 | ≤ 3 s |
| Crash-free sessions (mobile) | ≥ 99.5% |
| Monthly availability | ≥ 99.9% |
| D30 retention of activated users | ≥ 40% |
| Infra cost per MAU | ≤ €0.01 (commodity cloud profile) |

## 5. Target users

| Segment | Need served |
|---|---|
| Privacy-conscious consumers | E2EE everything, minimal metadata, no ads/tracking |
| Communities & organizations self-hosting | Own the infrastructure and the data; LAN/air-gap capable |
| Teams in low-connectivity environments | Offline-first clients, data-frugal protocols, PTT |

## 6. Explicitly not the product (V2)

No ads, no content analytics, no payments, no business/bots API, no federation. See [scope-and-competitors.md](scope-and-competitors.md).
