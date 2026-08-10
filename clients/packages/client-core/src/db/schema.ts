// Local SQLite schema (SQLCipher at rest). The server relays ciphertext and
// keeps only undelivered messages ≤30 days; the client owns durable history
// (offline-sync-local-store.md). `messages.msg_uuid` is the PK so at-least-once
// delivery de-duplicates on insert.

export const SCHEMA = `
CREATE TABLE IF NOT EXISTS conversations (
  id           TEXT PRIMARY KEY,
  title        TEXT    NOT NULL DEFAULT '',
  last_preview TEXT    NOT NULL DEFAULT '',
  last_seq     INTEGER NOT NULL DEFAULT 0,
  updated_at   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS messages (
  msg_uuid        TEXT PRIMARY KEY,
  conversation_id TEXT    NOT NULL,
  seq             INTEGER NOT NULL DEFAULT 0,
  sender          TEXT    NOT NULL DEFAULT '',
  kind            INTEGER NOT NULL DEFAULT 1,
  body            TEXT    NOT NULL DEFAULT '',
  deleted         INTEGER NOT NULL DEFAULT 0,
  mine            INTEGER NOT NULL DEFAULT 0,
  state           TEXT    NOT NULL DEFAULT 'received',
  pinned          INTEGER NOT NULL DEFAULT 0,
  starred         INTEGER NOT NULL DEFAULT 0,
  accepted_at     INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(conversation_id, seq);

CREATE TABLE IF NOT EXISTS outbox (
  client_ref      TEXT PRIMARY KEY,
  conversation_id TEXT    NOT NULL,
  payload         BLOB    NOT NULL,
  attempts        INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS cursors (
  conversation_id TEXT PRIMARY KEY,
  last_seq        INTEGER NOT NULL DEFAULT 0
);
`;
