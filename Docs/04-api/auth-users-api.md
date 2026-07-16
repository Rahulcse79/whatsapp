# REST API — Auth, Devices, Keys, Users, Contacts

| Doc | Endpoint specifications |
|---|---|
| Status | v1.0 · Conventions: [api-standards.md](api-standards.md) apply to every row |

## Auth (`/v1/auth`) — FR-AUTH-01…08

| Endpoint | Req → Resp | Errors | Notes |
|---|---|---|---|
| `POST /request-otp` | `{phone}` → `{challenge_id, channel}` | `RATE_LIMITED`, `VALIDATION_PHONE` | Channel = sms\|email per deployment profile; response identical whether number exists (no enumeration) |
| `POST /verify-otp` | `{challenge_id, code, device_info}` → `{access_jwt, refresh_token, device_id, requires_pin}` | `AUTH_OTP_INVALID`(≤5), `AUTH_OTP_EXPIRED`, `AUTH_PIN_REQUIRED` | Creates user+primary device on first registration |
| `POST /verify-pin` | `{challenge_id, pin}` → tokens | `AUTH_PIN_INVALID` (hard rate limit) | SIM-swap defense (Argon2id) |
| `POST /refresh` | `{refresh_token}` → rotated pair | `AUTH_REFRESH_REUSED` → session killed | Rotation with reuse detection |
| `POST /logout` | `{}` → 204 | — | Current device |
| `PUT /pin` | `{old?, new}` → 204 | `AUTH_PIN_INVALID` | Set/change 2FA PIN |

## Devices (`/v1/devices`) — FR-AUTH-05/06

| Endpoint | Req → Resp | Notes |
|---|---|---|
| `GET /` | → `[{id, name, platform, is_primary, last_active}]` | |
| `POST /link/init` | (new device, unauthenticated) → `{link_token, qr_payload}` | Step 1 of QR link |
| `POST /link/approve` | (primary) `{link_token, signed_device_cert}` → 204 | Primary signs device list — [e2ee-design.md](../06-security/e2ee-design.md) §5 |
| `PATCH /{id}` | `{name}` → 204 | |
| `DELETE /{id}` | → 204 | Revoke: kills tokens+prekeys+push route atomically; emits `user.events` |

## Keys (`/v1/keys`) — Signal distribution (public material only)

| Endpoint | Req → Resp | Notes |
|---|---|---|
| `PUT /prekeys` | `{signed_prekey, one_time_prekeys[100]}` → 204 | Device replenishes; server alerts client at < 20 remaining (WS hint) |
| `GET /bundle/{user_id}` | → `{devices: [{device_id, identity_key, signed_prekey, one_time_prekey?}]}` | Consumes one-time prekeys atomically; rate-limited (enumeration defense); read-replica served |

## Users (`/v1/users`) — FR-USER-*

| Endpoint | Req → Resp | Notes |
|---|---|---|
| `GET /me` / `PATCH /me` | profile CRUD | username uniqueness `VALIDATION_USERNAME_TAKEN` |
| `PUT /me/privacy` | `{last_seen, avatar, about, read_receipts}` each `everyone\|contacts\|nobody` → 204 | Enforced server-side at presence-sub & receipt paths |
| `GET /{id}/profile` | → fields the requester may see | Privacy-filtered server-side |
| `POST /blocks` / `DELETE /blocks/{user_id}` | → 204 | Block semantics: FR-USER-03 |
| `GET /me/export` | → job → downloadable encrypted archive | GDPR (FR-AUTH-08) |
| `DELETE /me` | `{pin?}` → 204 | Tombstone now, purge ≤ 30 d |

## Contacts (`/v1/contacts`) — FR-CONT-*

| Endpoint | Req → Resp | Notes |
|---|---|---|
| `POST /sync` | `{hashes: [hmac(phone)]}` → `{matched: [{hash, user_id, username}]}` | Peppered HMAC; 4/day limit; ≤ 5k hashes/call — enumeration defenses in [threat-model-abuse.md](../06-security/threat-model-abuse.md) |
| `GET /search?u=` | → `[{user_id, username}]` | Metadata search (PG trigram), rate-limited |
| `PUT /favorites/{user_id}` / `DELETE` | → 204 | |
