# REST API — Media & Stories

| Doc | Upload/download orchestration, stories |
|---|---|
| Status | v1.0 · Blobs are **always ciphertext**; media-svc never sees plaintext or keys (file keys travel in the E2EE envelope) |

## Media (`/v1/media`) — FR-MED-*

| Endpoint | Req → Resp | Errors / notes |
|---|---|---|
| `POST /uploads` | `{size, content_hash, mime_claimed}` → `{upload_id, object_key, part_urls[], part_size}` | `VALIDATION_TOO_LARGE`(25MB), `RATE_LIMITED`(30/hr), quota check (gRPC→core-api). Multipart presigned PUTs, direct-to-MinIO |
| `POST /uploads/{id}/presign` | `{missing_parts[]}` → fresh URLs | **The resumable-upload primitive** (FR-MED-04) |
| `POST /uploads/{id}/complete` | `{parts_etags[]}` → `{media_id}` | Verifies size+hash; registers `media_objects` refcount 0; refcount++ when a message referencing it is accepted |
| `POST /download-urls` | `{object_keys[]}` → `[{key, url, expires}]` | Short-TTL presigned GETs; membership-of-referencing-conversation check |
| GIF proxy: `GET /gif/search?q=&limit=` | → `{results:[{id,url,preview_url,w,h}]}` | Server-side proxy — client IP never reaches provider (FR-MED-05); per-user rate-limited; `FEATURE_DISABLED` in air-gap profile |
| Stickers: `GET /stickers/packs` | → `{packs[]}` | Public catalog (no per-pack contents); local packs survive air-gap |
| `GET /stickers/packs/{id}` | → `{id,title,author,animated,stickers[]}` | Pack detail; `STICKER_PACK_NOT_FOUND` |
| `GET /stickers/installed` | → `{packs[]}` | Caller's installed set |
| `POST` / `DELETE /stickers/packs/{id}/install` | → 204 | Install / uninstall for the caller (idempotent) |

Stickers & GIFs are **public** catalog content — a sticker's `object_key` is a shared (non-E2EE) asset, distinct from the per-user ciphertext in `media_objects`.

Lifecycle: `expires_at = last_ref + 30 d`; GC job sweeps `refcount = 0` + expired (`media.lifecycle` events); MinIO ILM as backstop.

## Stories (`/v1/stories`) — FR-STORY-*

| Endpoint | Req → Resp | Notes |
|---|---|---|
| `POST /` | `{media_ref?, kind, audience_override?}` → `{story_id, expires_at}` | Audience snapshot computed at post time; per-story key distributed via WS `MsgSend{kind: STORY_KEY}` pairwise — server never holds it |
| `GET /feed` | → `[{story_id, author, expires_at, key_available}]` | Only stories whose audience includes caller |
| `POST /{id}/view` | → 204 | E2EE view receipt relayed to author; counts aggregate client-side |
| `GET /{id}/viewers` | (author) → viewer list | From `story_views` |
| `DELETE /{id}` | → 204 | Early delete; 24 h hard-expiry job + MinIO lifecycle backstop regardless |

## Encrypted backups (`/v1/backups`) — FR-SYNC-04

| Endpoint | Notes |
|---|---|
| `POST /` → presigned multipart | Client-encrypted blob (Argon2id-derived key); server sees ciphertext + size only |
| `GET /latest` → presigned GET | Restore path; key never leaves the user |
| Quota | 1 active backup/user; size cap per deployment config |
