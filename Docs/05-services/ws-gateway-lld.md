# LLD — ws-gateway

| Doc | Connection tier: registry, resume, backpressure, drain |
|---|---|
| Status | v1.0 · Go; stateless; ~20k conns/pod (2 vCPU/4 GB); 3 pods for HA |

## 1. Responsibilities — and hard non-responsibilities

Owns: WSS termination (behind Envoy), auth-at-connect, connection registry, frame routing, inbox replay orchestration, receipt coalescing relay, presence/typing relay, drain.
**Never:** business logic, PG access, content inspection. A gateway is a smart pipe; all decisions live in core-api.

## 2. Internal structure

```
conn goroutines (2/conn: read-pump, write-pump)
   │ decoded frames
   ▼
frame router ──► session frames    → local handling (ping, resume)
             ──► msg/receipt/etc   → gRPC core-api (2 s timeout, 1 retry)
             ──► presence/typing   → Valkey direct (latency; no business rules)
registry: map[deviceID]*conn sharded ×256 (DS&A §5)
NATS consumer: DELIVERY filtered to this pod's routed devices → write-pump queues
```

- **Read pump:** decode, size-cap 256 KB, per-conn GCRA, frame-type dispatch.
- **Write pump:** single writer per conn (no write races); 1 MB high-water → pause NATS pulls for that device (pull-based consumer = natural backpressure; backlog stays in inbox/NATS, never pod memory).

## 3. Connect / resume sequence (implementation-level)

```
1. WS upgrade (Envoy already did TLS + edge limits)
2. Hello{jwt, device_id, resume_token?, cursors?}
3. verify JWT locally (public key cached, kid-rotated) — no core-api round trip
4. SET route:{device}=pod EX 90          (last-writer-wins; old conn gets 4409)
5. subscribe DELIVERY dev.{device}.out
6. resume? validate resume:{device} hash → replay:
     gRPC core-api.PullInbox(device, cursors, batch=100) → InboxBatch frames
     loop until drained; interleave-safe: live msgs queue behind replay per conv
7. HelloAck{new resume_token (rotated), replayed: true}
8. steady state: heartbeat 30 s ± jitter → refresh route TTL + presence
```

**Interleave rule:** during replay, live `dev.out` messages for a conversation buffer until that conversation's replay cursor passes them (per-conv seq comparison) — client never sees out-of-order seq.

## 4. Disconnect & failure

| Event | Handling |
|---|---|
| Clean close | delete route (if still ours — compare pod id), presence offline after 15 s grace, unsubscribe |
| Pod crash | routes self-expire ≤ 90 s; NATS redelivers unACKed to wherever the device reconnects; **zero loss** (inbox) |
| Duplicate connect (same device) | new conn wins route; old conn closed 4409 |
| Reconnect storm (deploy/net-flap) | client jittered backoff (1s→30s cap) + resume tokens skip full auth + Envoy edge limits; 3 small pods bound blast radius |

## 5. Drain (zero-downtime deploys — NFR-14)

SIGTERM → stop accepting (readiness probe fails) → send `ServerHint{DRAIN, reconnect_after: jitter(0–60s)}` to all conns in random order over 60 s → close stragglers at 120 s → exit. Session resume makes the hop invisible: worst case is one replay round trip.

## 6. Observability specifics

Per-pod gauges: conns, conns_by_platform, NATS pending, write-buffer high-water count, replay depth. Per-frame histograms: routing latency, gRPC latency. Every frame carries trace context → one trace spans client-send → gateway → core-api → NATS → recipient gateway → client-deliver ([monitoring doc](../09-observability/monitoring-logging-tracing.md)).

## 7. Sizing math

20k conns × 2 goroutines × ~8 KB stack ≈ 320 MB + buffers ≈ 1.3 GB budget on a 4 GB pod (headroom for replay bursts). CPU is protobuf encode + TLS: ~1.5 vCPU at 1,200 deliveries/s. Vertical ceiling ~250k conns/pod exists but blast radius argues for horizontal past 50k (HLD §16.1).
