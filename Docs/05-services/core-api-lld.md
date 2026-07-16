# LLD — core-api (Modular Monolith)

| Doc | Low-level design: module layout, interfaces, transactions |
|---|---|
| Status | v1.0 · Contexts: auth, keys, chat, groups, media(quota), calls+ptt, stories, presence, notify(emit), admin |

## 1. Package layout (the seams are the architecture)

```
server/
├── cmd/core-api/main.go            # wiring only: config, pools, servers, module init
└── internal/
    ├── auth/      keys/      chat/      groups/     calls/
    ├── ptt/       stories/   presence/  admin/      contacts/
    │   └── each context:
    │       domain/        # pure logic, no I/O imports (unit-testable in ms)
    │       port.go        # THE interface other contexts may call
    │       adapters/      # pg.go, valkey.go, nats.go implementing domain ports
    │       http.go        # REST handlers (thin: decode → domain → encode)
    │       grpc.go        # internal surface where needed
    ├── platform/          # cross-cutting: pg pool, valkey client, nats conn,
    │                      # otel, config, flags, ratelimit, idgen (UUIDv7)
    └── proto/gen/         # buf-generated types (never hand-edited)
```

**Import rules (lint-enforced, [coding-standards.md](../13-standards/coding-standards.md)):** contexts import other contexts **only** via `port.go`; `domain/` imports no adapters and no platform I/O; `platform/` imports no contexts. Violations fail CI — these rules are what keeps extraction (microservices.md §5) a deploy change.

## 2. The hot path: chat accept (target < 10 ms server time)

```
gRPC AcceptMessage(sealed_frame)
 1. authz: sender device active, conversation membership (cached, ver-checked)
 2. Valkey SET dedupe:{uuid} NX EX 86400        → duplicate? return prior ack
 3. rate check (GCRA)                            → RATE_LIMITED w/ retry_after
 4. overlay? validate window vs server accept-time index (edit≤15m, del≤48h)
 5. BEGIN;
      UPDATE conversations SET seq=seq+1 … RETURNING seq;
      INSERT INTO message_inbox (… 1 row per recipient device ≤ small conv,
                                  or sender-row only + async fan-out ≥ 16 devices);
    COMMIT;
 6. publish dev.{id}.out for online routes; push.dispatch for rest
 7. return MsgAck{seq}
```

Steps 5–6 non-atomic by design: publish failure → NATS retry/redelivery; consumer dedupe absorbs. Inbox row is the delivery guarantee, not the publish.

## 3. Fan-out worker (groups ≥ 16 devices)

Bounded pool (N=8 workers, queue depth SLI-monitored); batch INSERT 500 rows/statement; membership from Valkey versioned cache; emits per-device publishes as batches commit. Sender's ACK already returned at accept (durable intent row) — HLD §8.3.

## 4. Module notes (what's non-obvious per context)

| Context | Non-obvious decisions |
|---|---|
| auth | OTP hashes only (never codes); challenge state in PG (survives pod death); JWT: EdDSA, kid-rotated; refresh rotation w/ reuse → kill session + alert user devices |
| keys | Bundle fetch consumes one-time prekey in the same tx (no double-hand-out); low-water WS hint at <20 |
| groups | Every membership/settings mutation bumps `groups.version` + emits ordered `group.events.{id}` — clients rotate Sender Keys on these events; server never touches keys |
| calls | Ring machine is a PG row + timer wheel (survives pod death via takeover scan); LiveKit webhooks reconcile room truth |
| ptt | All floor logic in Valkey Lua ([valkey-keyspace.md](../03-database/valkey-keyspace.md) §2); this module just translates WS↔Lua↔LiveKit-perm-API |
| presence | Logic here, state in Valkey; privacy filter applied at subscribe time (never ships data to unauthorized subscribers) |
| stories | Audience snapshot materialized at post (uuid[]); feed query = GIN on snapshot |
| admin | Separate router + middleware chain (OIDC, IP allowlist, audit interceptor — every mutating call writes audit_log in same tx) |

## 5. Config & flags

All config via env + mounted files (12-factor); no config in code. Feature flags evaluated via platform/flags with 30 s Valkey-cached rules; kill-switches exist for: media uploads, new registrations, group creation, calls — each an operational circuit breaker for incident response.

## 6. Testing hooks

Every port has a fake in `internal/<ctx>/<ctx>test/`; domain tests never touch I/O; adapter tests run against Testcontainers (PG/Valkey/NATS) — see [test-strategy.md](../10-testing/test-strategy.md).
