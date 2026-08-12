# Database Design — PostgreSQL 17

| Doc | Full logical schema, ERD, partitioning, indexes |
|---|---|
| Status | v1.0 |
| Upstream | [/HLD.md](../../HLD.md) §7 |
| Rules | UUIDv7 PKs; `created_at timestamptz` everywhere; **no plaintext content columns exist anywhere**; DDL below is the reviewable contract — migrations implement it expand–contract |

## 1. ERD (logical)

```
users 1──∞ devices 1──∞ prekeys            users 1──∞ push_tokens (via devices)
  │            └──∞ signed_prekeys           │
  ├─∞ contacts (hashed)   ├─∞ blocks         ├─∞ stories 1──∞ story_views
  │                       │                  └─∞ reports
conversations 1──∞ message_inbox (per recipient DEVICE, ciphertext, TTL)
  │
groups 1──∞ group_members ∞──1 users;  groups 1──∞ invite_links
media_objects (refcounted, TTL) ──► MinIO blobs (ciphertext)
call_records (metadata, 90d) · audit_log (append-only) · feature_flags
```

## 2. Core tables (DDL-level contract)

```sql
-- ═══ identity ═══
CREATE TABLE users (
  id              uuid PRIMARY KEY,                 -- UUIDv7
  phone_hash      bytea NOT NULL UNIQUE,            -- HMAC(pepper, E.164); pepper in KMS/SOPS
  phone_enc       bytea,                            -- AES-GCM for SMS sending only; NULL in offline profile
  username        citext UNIQUE,
  display_name    text,
  about           text,
  avatar_ref      uuid REFERENCES media_objects(id),
  privacy         jsonb NOT NULL DEFAULT '{}',      -- {last_seen,avatar,about,read_receipts}: everyone|contacts|nobody
  pin_hash        text,                             -- Argon2id 2FA registration PIN
  status          smallint NOT NULL DEFAULT 0,      -- 0 active, 1 suspended, 2 deleted(tombstone)
  created_at      timestamptz NOT NULL DEFAULT now(),
  deleted_at      timestamptz
);

CREATE TABLE devices (
  id              uuid PRIMARY KEY,
  user_id         uuid NOT NULL REFERENCES users(id),
  is_primary      boolean NOT NULL,
  platform        smallint NOT NULL,                -- ios|android|web
  device_name     text,
  identity_key    bytea NOT NULL,                   -- public Signal identity key
  cert            bytea NOT NULL,                   -- signed by user's primary identity key
  registered_at   timestamptz NOT NULL DEFAULT now(),
  last_active_at  timestamptz,
  revoked_at      timestamptz,
  CONSTRAINT max_devices EXCLUDE USING gist (user_id WITH =) WHERE (false) -- enforced in app tx: 1 primary + ≤4 linked
);
CREATE UNIQUE INDEX one_primary_per_user ON devices(user_id) WHERE is_primary AND revoked_at IS NULL;

-- ═══ signal key distribution (public material only) ═══
CREATE TABLE signed_prekeys (
  device_id uuid REFERENCES devices(id), key_id int, pubkey bytea NOT NULL,
  signature bytea NOT NULL, created_at timestamptz DEFAULT now(),
  PRIMARY KEY (device_id, key_id));
CREATE TABLE prekeys (         -- one-time prekeys, consumed on session setup
  device_id uuid REFERENCES devices(id), key_id int, pubkey bytea NOT NULL,
  consumed_at timestamptz, PRIMARY KEY (device_id, key_id));
CREATE INDEX prekeys_available ON prekeys(device_id) WHERE consumed_at IS NULL;

-- ═══ messaging ═══
CREATE TABLE conversations (
  id        uuid PRIMARY KEY,
  kind      smallint NOT NULL,                      -- 0 direct | 1 group
  group_id  uuid REFERENCES groups(id),
  direct_key text,                                  -- (migration 000011) sorted user pair for 1:1 dedup; UNIQUE where not null
  seq       bigint NOT NULL DEFAULT 0,              -- per-conversation monotonic sequence
  created_at timestamptz NOT NULL DEFAULT now());

CREATE TABLE conversation_members (                  -- (migration 000011) membership for direct convs; groups use group_members
  conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE,
  user_id uuid REFERENCES users(id) ON DELETE CASCADE,
  added_at timestamptz DEFAULT now(),
  PRIMARY KEY (conversation_id, user_id));           -- recipient resolution joins this to devices in the accept tx

CREATE TABLE message_inbox (                        -- THE hot table; relay buffer, delete-on-ACK
  recipient_device_id uuid NOT NULL,
  conversation_id     uuid NOT NULL,
  seq                 bigint NOT NULL,              -- conversation seq
  msg_uuid            uuid NOT NULL,                -- client UUIDv7 (idempotency)
  sender_device_id    uuid NOT NULL,
  kind                smallint NOT NULL,            -- MsgKind proto enum (text|media|overlay_*|reaction|pin|…)
  overlay_target      uuid,                         -- original msg for overlay kinds
  ciphertext          bytea NOT NULL,               -- sealed envelope incl. enc thumbnail if media
  accepted_at         timestamptz NOT NULL DEFAULT now(),  -- overlay-window validation + InboxItem.accepted_at_ms
  expires_at          timestamptz NOT NULL,         -- accepted_at + 30d
  PRIMARY KEY (recipient_device_id, conversation_id, seq, msg_uuid)
) PARTITION BY HASH (recipient_device_id);          -- 16 hash partitions (migration 000005)
-- The PK serves the replay/resume scan (device, conversation, seq-range).
-- Rev note (v1.1 of this doc): the earlier covering INCLUDE(ciphertext) index
-- was WRONG — ciphertext runs to 256 KB and would blow the index-tuple limit;
-- hot heap fetches off the PK are the correct plan.
CREATE INDEX inbox_expiry ON message_inbox (expires_at);   -- TTL sweeper scan
-- Monthly RANGE sub-partitioning (pg_partman) is a planned later ADD, taken
-- when delete-job cost shows in metrics — see server/migrations/README.md.

-- ═══ groups ═══
CREATE TABLE groups (
  id uuid PRIMARY KEY, name text NOT NULL, description text,
  avatar_ref uuid REFERENCES media_objects(id),
  settings jsonb NOT NULL DEFAULT '{}',             -- {who_can_post, who_can_edit_info, announcements}
  version bigint NOT NULL DEFAULT 0,                -- bumped on every membership/settings change
  created_by uuid REFERENCES users(id), created_at timestamptz DEFAULT now());
CREATE TABLE group_members (
  group_id uuid REFERENCES groups(id), user_id uuid REFERENCES users(id),
  role smallint NOT NULL DEFAULT 0,                 -- member|admin|owner
  joined_at timestamptz DEFAULT now(), PRIMARY KEY (group_id, user_id));
CREATE INDEX members_by_user ON group_members(user_id);
CREATE TABLE invite_links (
  token text PRIMARY KEY, group_id uuid REFERENCES groups(id),
  created_by uuid, expires_at timestamptz, revoked_at timestamptz, max_uses int, uses int DEFAULT 0);

-- ═══ media / stories ═══
CREATE TABLE media_objects (
  id uuid PRIMARY KEY, object_key text NOT NULL UNIQUE,   -- MinIO key
  size_bytes bigint NOT NULL, content_hash bytea NOT NULL,
  uploader_user_id uuid NOT NULL, refcount int NOT NULL DEFAULT 0,
  upload_state smallint NOT NULL DEFAULT 0,               -- pending|complete|gc_candidate
  created_at timestamptz DEFAULT now(), expires_at timestamptz);
CREATE INDEX media_gc ON media_objects(expires_at) WHERE refcount = 0;

CREATE TABLE stories (
  id uuid PRIMARY KEY, author_id uuid REFERENCES users(id),
  media_ref uuid REFERENCES media_objects(id),
  audience_snapshot uuid[] NOT NULL,                       -- eligible user ids at post time
  expires_at timestamptz NOT NULL,                         -- post + 24h, hard delete
  created_at timestamptz DEFAULT now());
CREATE TABLE story_views (story_id uuid, viewer_id uuid, viewed_at timestamptz DEFAULT now(),
  PRIMARY KEY (story_id, viewer_id));

-- Sticker packs (migration 000012, FR-MED-05). PUBLIC catalog assets — NOT E2EE
-- media_objects: a sticker's object_key points at a shared blob. Local packs are
-- the offline fallback when the GIF proxy is disabled (air-gap profile).
CREATE TABLE sticker_packs (
  id text PRIMARY KEY, title text NOT NULL, author text NOT NULL DEFAULT '',
  tray_key text NOT NULL DEFAULT '', animated boolean NOT NULL DEFAULT false,
  created_at timestamptz DEFAULT now());
CREATE TABLE stickers (
  id text PRIMARY KEY, pack_id text NOT NULL REFERENCES sticker_packs(id) ON DELETE CASCADE,
  emoji text NOT NULL DEFAULT '', object_key text NOT NULL, position int NOT NULL DEFAULT 0);
CREATE INDEX stickers_by_pack ON stickers(pack_id, position);
CREATE TABLE user_sticker_packs (                        -- per-user install set (idempotent PK)
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  pack_id text NOT NULL REFERENCES sticker_packs(id) ON DELETE CASCADE,
  installed_at timestamptz DEFAULT now(), PRIMARY KEY (user_id, pack_id));
CREATE INDEX user_sticker_packs_by_user ON user_sticker_packs(user_id);

-- ═══ contacts / social ═══
CREATE TABLE contacts (owner_id uuid, contact_phone_hash bytea, matched_user_id uuid,
  favorite boolean DEFAULT false, PRIMARY KEY (owner_id, contact_phone_hash));
CREATE TABLE blocks (blocker_id uuid, blocked_id uuid, created_at timestamptz DEFAULT now(),
  PRIMARY KEY (blocker_id, blocked_id));

-- ═══ calls / push ═══
CREATE TABLE call_records (
  id uuid PRIMARY KEY, room_id text NOT NULL, kind smallint,          -- voice|video|ptt
  initiator uuid NOT NULL, participants uuid[] NOT NULL,
  started_at timestamptz, ended_at timestamptz,
  outcome smallint NOT NULL);                                          -- completed|missed|declined|failed
CREATE INDEX calls_by_user ON call_records USING gin(participants);   -- 90d retention job
CREATE TABLE push_tokens (device_id uuid PRIMARY KEY REFERENCES devices(id),
  provider smallint NOT NULL,                                          -- fcm|apns|apns_voip|ntfy|webpush
  token text NOT NULL, updated_at timestamptz DEFAULT now(), failing_since timestamptz);

-- ═══ trust & safety / admin ═══
CREATE TABLE reports (id uuid PRIMARY KEY, reporter_id uuid, target_user_id uuid,
  reason smallint, note text, disclosed_ciphertext bytea,             -- ONLY with reporter consent
  state smallint DEFAULT 0, created_at timestamptz DEFAULT now());
CREATE TABLE audit_log (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  actor uuid NOT NULL, action text NOT NULL, target uuid, reason text,
  at timestamptz NOT NULL DEFAULT now());                              -- append-only: no UPDATE/DELETE grants to anyone
CREATE TABLE feature_flags (flag text PRIMARY KEY, rules jsonb NOT NULL, updated_by uuid, updated_at timestamptz);

-- ═══ abuse control ═══
CREATE TABLE otp_attempts (phone_hash bytea, at timestamptz, success boolean,
  ip inet, PRIMARY KEY (phone_hash, at));                              -- 10-min TTL sweep; long windows in Valkey

-- ═══ auth runtime state (added v1.2 of this doc, migration 000009) ═══
CREATE TABLE otp_challenges (                    -- OTP state survives pod death (core-api-lld §4)
  id uuid PRIMARY KEY, phone_hash bytea, code_hash bytea, salt bytea,  -- code stored ONLY as HMAC(salt, code)
  channel smallint, attempts smallint DEFAULT 0,                        -- max 5
  verified_at timestamptz, pin_pending boolean DEFAULT false,
  created_at timestamptz, expires_at timestamptz);                      -- 10-min TTL

CREATE TABLE sessions (                          -- refresh rotation + reuse detection (§15.2 HLD)
  id uuid PRIMARY KEY, device_id uuid REFERENCES devices(id),
  refresh_hash bytea UNIQUE,                     -- SHA-256 of current refresh token
  rotated_from bytea,                            -- previous hash: presenting it again = reuse → session killed
  created_at timestamptz, last_used_at timestamptz,
  expires_at timestamptz, revoked_at timestamptz);

CREATE TABLE device_links (                      -- QR multi-device linking handoff (migration 000010, e2ee-design §5)
  link_token text PRIMARY KEY,                   -- high-entropy secret shown in the QR + polled with
  platform smallint, device_name text, identity_key bytea,  -- the NEW device's public identity key
  state smallint DEFAULT 0,                       -- 0 pending | 1 approved | 2 consumed
  approved_by uuid, user_id uuid, device_id uuid, cert bytea,  -- set at primary approval
  created_at timestamptz, expires_at timestamptz);  -- ~5-min TTL
```

## 3. Partitioning & lifecycle

| Table | Strategy | Purge |
|---|---|---|
| `message_inbox` | HASH(recipient_device_id) ×16 (monthly RANGE sub-partitions via pg_partman = planned later ADD) | ACK-delete (hot path) + TTL sweeper on `inbox_expiry`; `DROP PARTITION` once partman lands |
| `call_records` | monthly RANGE | drop partitions > 90 d |
| `stories` | none (small) | hard-delete job hourly + MinIO lifecycle backstop |
| `otp_attempts` | none | 10-min sweeper |
| `audit_log` | yearly RANGE | never purged; archived |

## 4. Index review (every index justifies itself)

| Index | Serves | Verdict |
|---|---|---|
| `message_inbox` PK | resume/sync replay scan — the system's most critical read | required (covering INCLUDE rejected — oversized bytea, see rev note above) |
| `one_primary_per_user` partial unique | device invariants | required |
| `prekeys_available` partial | bundle fetch (hot on new sessions) | required |
| `members_by_user` | "my groups", fan-out membership loads | required |
| `calls_by_user` GIN | call history queries | acceptable (low write rate) |
| `users(username)` via UNIQUE citext | username search (+ separate `pg_trgm` GIN for fuzzy) | required |
| trigram GIN on `groups.name` | metadata search (ADR-005) | required |

**Banned:** indexes on `message_inbox` beyond the two listed (write amplification on the hot path needs justification per addition).

## 5. Access & pooling

- **PgBouncer transaction pooling from day one**; per-deployable pool budgets (bulkhead — see design-patterns doc).
- Roles: `core_api_rw` (app tables), `media_rw` (`media_objects`), `notify_rw` (`push_tokens`), `admin_ro`+`audit_append`. `audit_log`: INSERT-only for every role — revoke UPDATE/DELETE at the grant level.
- Read replicas take: prekey bundle fetches, profile reads, group metadata reads (lag-tolerant paths only; receipts/inbox never).

## 6. What is deliberately absent

No `messages` table. No content columns. No plaintext phone numbers in indexes (`phone_hash` only). No foreign key from `message_inbox` to `users` (partition-locality + volume; integrity owned by accept-path validation). No triggers on the hot table (explicit app-side writes only).
