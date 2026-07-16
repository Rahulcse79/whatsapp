# API Standards & Conventions

| Doc | Binding conventions for all REST/WS/gRPC surfaces |
|---|---|
| Status | v1.0 |
| Scope | REST (client↔core-api/media-svc), WS (client↔ws-gateway), gRPC + NATS (internal) |

## 1. General

- **Base path:** `/v1/…`. Versioning by URL major only; breaking changes require `/v2` + 6-month dual-running. Protobuf schemas are append-only (buf breaking-change check in CI).
- **IDs:** UUIDv7 strings. **Timestamps:** RFC 3339 UTC. **Payloads:** JSON over REST (protobuf on WS/gRPC).
- **Auth:** `Authorization: Bearer <access JWT (10 min)>`; refresh via `/v1/auth/refresh` (rotating, reuse-detected). WS authenticates once at `hello`, then per-connection identity.
- **Idempotency:** all mutating POSTs accept client UUIDv7 either in body (`msg_uuid`) or `Idempotency-Key` header; server dedupes 24 h.

## 2. Error envelope (uniform, see design-patterns doc §2)

```json
{ "error": { "code": "RATE_LIMITED", "message": "developer-facing text",
             "retryable": true, "retry_after_ms": 4000, "trace_id": "…" } }
```
HTTP mapping: 400 `VALIDATION_*` · 401 `AUTH_*` · 403 `STATE_*`(perm) · 404 · 409 `STATE_*`(conflict) · 422 domain rejects · 429 `RATE_*` · 5xx `TRANSIENT_*`/`INTERNAL`.

## 3. Pagination

Cursor-based only (`?cursor=<opaque>&limit=50`, response carries `next_cursor`). Offset pagination is banned (inbox/partition unfriendly, unstable under writes).

## 4. Rate limits (edge-enforced, GCRA; per-route overrides)

| Scope | Default |
|---|---|
| Per IP (edge) | 100 req/s burst 200 |
| Per device REST | 30 req/s |
| OTP request | 3/hr, 5/day per number; 10/day per IP |
| Message send (WS) | 20/s per device; new accounts graduated |
| Group send (≥ 256 members) | 2/s per sender |
| Media create-upload | 30/hr per user |
| Contact sync | 4/day per device |

Every 429 carries `retry_after_ms`. Limits are config, not code (feature-flag service).

## 5. Security requirements on every surface

TLS 1.3 only; HSTS; certificate pinning in mobile clients. No PII in URLs or query strings (IDs are fine — they're opaque UUIDs). No secrets/tokens in logs. CORS: web origin allowlist, credentials never wildcarded. All admin routes: separate hostname + IP allowlist + OIDC (HLD §15.6).

## 6. REST surface inventory (details per domain doc)

| Domain | Doc |
|---|---|
| Auth, devices, keys, users, contacts | [auth-users-api.md](auth-users-api.md) |
| Groups + messaging semantics | [messaging-groups-api.md](messaging-groups-api.md) |
| Media, stories | [media-stories-api.md](media-stories-api.md) |
| Calls, PTT | [calls-ptt-api.md](calls-ptt-api.md) |
| WS frame protocol | [websocket-protocol.md](websocket-protocol.md) |
| Internal NATS subjects | [internal-events-nats.md](internal-events-nats.md) |

## 7. Documentation contract

Every endpoint documents: method+path, auth, request/response schema, error codes it can return, rate limit, idempotency behavior, and the FR it implements. An endpoint without an FR reference does not ship.
