# Sequence Diagrams — Core Flows

| Doc | End-to-end sequences for every critical flow |
|---|---|
| Status | v1.0 |
| Convention | ASCII sequences; each maps to protocol tests in [10-testing/test-strategy.md](../10-testing/test-strategy.md) |

## 1. Registration & first login

```
Client            core-api(auth)        SMS/Email provider     Valkey/PG
  │ POST /auth/request-otp  │                  │                  │
  │────────────────────────►│ rate-check (3/hr,5/day per number)  │
  │                         │─────────────────►│ send OTP         │
  │                         │ store otp hash + attempts, TTL 10m ►│
  │ POST /auth/verify-otp   │                  │                  │
  │────────────────────────►│ verify (≤5 attempts)                │
  │                         │ if re-registration & 2FA PIN set → require PIN
  │                         │ create user (if new) + device row   │
  │ ◄── access JWT (10m) + refresh token + device_id ─────────────│
  │ POST /keys/prekeys (identity, signed-prekey, 100 one-time)    │
  │────────────────────────►│ store public bundles ──────────────►│
```

## 2. 1:1 message (happy path + offline branch) — canonical (HLD §8.2)

```
Sender          ws-gateway       chat(core-api)      NATS/PG           Recipient
  │ encrypt per device │               │               │                  │
  │ frame msg.send ───►│ authz, gRPC ─►│ dedupe(Valkey)│                  │
  │  (UUIDv7)          │               │ conv seq++    │                  │
  │                    │               │ INSERT inbox rows (batch) ──────►│
  │                    │               │ publish dev.{id}.out ───────────►│ (online)
  │ ◄── ack "sent" ────│◄──────────────│               │ deliver ────────►│
  │                    │               │               │ ◄─ receipt ──────│
  │ ◄─ "delivered" ────│◄─ delete inbox row ◄──────────│                  │
  │                                                                       │
  │            [recipient offline] → NATS push.dispatch → notification-svc → FCM/APNs/ntfy
  │            data-only push → device wakes → WS connect → resume replay (see §5)
```

## 3. Group send with Sender Keys (HLD §8.3)

```
Sender          core-api(chat)         PG                NATS              N members
  │ encrypt ONCE (Sender Key) │         │                 │                  │
  │ msg.send(group) ─────────►│ validate membership+permissions              │
  │ ◄── ack "sent" (durable accept — BEFORE fan-out)      │                  │
  │                           │ async: batch INSERT N inbox rows ───────────►│
  │                           │ publish N× dev.{id}.out ──────-─────────────►│ online subset
  │                           │ push.dispatch for offline subset ───────────►│
  │ membership change ──────► │ group.event → ALL clients rotate Sender Key  │
```

## 4. Media upload (resumable, E2EE — HLD §9)

```
Client               media-svc              MinIO               core-api
  │ compress → thumbnail/blurhash → AES-256-GCM encrypt (per-file key)
  │ POST /media/uploads ───►│ validate type/25MB/quota            │
  │ ◄─ upload_id + multipart presigned URLs ─│                    │
  │ PUT chunks (parallel, direct) ──────────►│                    │
  │  [interrupted] → POST /uploads/{id}/presign?missing → resume  │
  │ POST /uploads/{id}/complete ►│ verify size+hash; register media_objects
  │ msg.send with {object_key, file_key, hash, enc_thumb} via WS ────► recipients
  │                                          │◄─ presigned GET ── recipient downloads, verifies hash, decrypts
```

## 5. Reconnect & session resume (the durability story — HLD §8.1)

```
Client                  ws-gateway            Valkey        PG inbox
  │ WSS connect + resume{token, last_cursor} │                │
  │──────────────────────►│ validate resume token (skips full auth)
  │                       │ route:{device}=this pod, TTL 90s ►│
  │                       │ subscribe dev.{id}.out            │
  │                       │ replay inbox WHERE seq > cursor ──►│ (batched, ordered)
  │ ◄── missed messages ──│                                   │
  │ ack per batch ───────►│ delete acked rows ────────────────►│
  │ live traffic resumes; invariant: server-ACKed msgs can never be lost
```

## 6. 1:1 call setup (HLD §10.3)

```
Caller        call-ctl(core-api)      LiveKit       notification-svc     Callee
  │ call.offer ──►│ create room, mint join JWTs        │                  │
  │               │ ws call.offer ────────────────────────────────-─────► │ (online)
  │               │ push.dispatch VoIP ───────────────►│ PushKit/FCM ───► │ (locked)
  │ ◄─ ringing ───│         [45s timeout → missed-call flow]              │
  │               │                    │ ◄────── callee joins room ────── │ answer
  │ join room ───►│ ─────────────────► │ ICE (STUN→TURN) · SRTP + SFrame E2EE
  │ ◄════════════ media via SFU ══════►│◄════════════════════════════════►│
  │ end ─────────►│ persist call_record (metadata only, 90d)              │
```

## 7. PTT floor acquisition (HLD §11)

```
Speaker         ws-gateway      core-api(ptt)        Valkey            LiveKit      Listeners
  │ ptt.request ───►│ ────────────►│ atomic Lua acquire ─►│              │            │
  │                 │              │ {holder,fence#,exp}  │              │            │
  │ ◄─ ptt.grant(fence#) ─────────│ flip publish perm ──────────────────►│            │
  │ unmute pre-negotiated track ══════════════════════════════════════════ RTP ═════► │ ≤200ms total
  │ heartbeat/500ms ►│ ───────────►│ refresh TTL ────────►│              │            │
  │ [2 missed beats] │             │ auto-release → grant next in FIFO queue          │
  │ [stale speaker with old fence#] → publish perm already revoked → audio dead (fencing)
```

## 8. Device linking (multi-device bootstrap — HLD §7.3)

```
New device        core-api           Primary device
  │ show QR {link_token, new_device_pubkey}     │
  │                  │ ◄── scan QR ─────────────│ user approves
  │                  │ ◄─ signed device-list update (primary identity key)
  │ ◄─ device registered; JWT issued ───────────│
  │ ◄══ E2E-encrypted history transfer (relayed ciphertext chunks) ══│
  │ publish own prekey bundles ─►│              │
  │ contacts see device-list change; sessions established per new device
```

## 9. Delete-for-everyone (overlay event — HLD §8.4)

```
Sender → msg.delete{target_uuid} → chat: validate sender==author AND age≤48h
  (content invisible to server — validation is metadata-only)
  → fan out like a message → recipients' clients tombstone local copy
  → clients that never got the original just drop the orphan overlay
```

## 10. Account recovery with 2FA PIN (SIM-swap defense)

```
Attacker w/ hijacked number → request-otp → verify-otp OK
  → server: account has 2FA PIN → require PIN (Argon2id verify, rate-limited hard)
  → FAIL → re-registration blocked; alert pushed to all existing devices
  → legit user: after N days of PIN-less inactivity policy, recovery = support flow (never automatic)
```
