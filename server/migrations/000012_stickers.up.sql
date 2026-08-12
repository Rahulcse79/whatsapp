-- Sticker packs (FR-MED-05). Unlike media_objects (per-user E2EE ciphertext),
-- stickers are shared PUBLIC catalog assets: object_key points at a shared blob,
-- not at encrypted media. Local packs are the offline fallback the air-gap
-- profile keeps when the GIF proxy is disabled (media-stories-api.md).

CREATE TABLE sticker_packs (
    id         text PRIMARY KEY,               -- stable slug (e.g. 'classic')
    title      text NOT NULL,
    author     text NOT NULL DEFAULT '',
    tray_key   text NOT NULL DEFAULT '',        -- object key of the tray icon
    animated   boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE stickers (
    id         text PRIMARY KEY,
    pack_id    text NOT NULL REFERENCES sticker_packs (id) ON DELETE CASCADE,
    emoji      text NOT NULL DEFAULT '',
    object_key text NOT NULL,                   -- shared (non-E2EE) asset key
    position   integer NOT NULL DEFAULT 0
);
-- Pack detail lists stickers in tray order.
CREATE INDEX stickers_by_pack ON stickers (pack_id, position);

-- Per-user install set. Composite PK makes install idempotent (ON CONFLICT).
CREATE TABLE user_sticker_packs (
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    pack_id      text NOT NULL REFERENCES sticker_packs (id) ON DELETE CASCADE,
    installed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, pack_id)
);
-- "which packs has this user installed" — the installed-list query.
CREATE INDEX user_sticker_packs_by_user ON user_sticker_packs (user_id);

-- Seed the built-in local packs shipped with every deployment (assets are
-- provisioned into the shared stickers bucket by ops; keys follow this layout).
INSERT INTO sticker_packs (id, title, author, tray_key, animated) VALUES
    ('classic', 'Classic',  'WhatsApp V2', 'stickers/classic/tray.webp', false),
    ('cats',    'Cats',     'WhatsApp V2', 'stickers/cats/tray.webp',    true);

INSERT INTO stickers (id, pack_id, emoji, object_key, position) VALUES
    ('classic-01', 'classic', '👍', 'stickers/classic/01.webp', 0),
    ('classic-02', 'classic', '😂', 'stickers/classic/02.webp', 1),
    ('classic-03', 'classic', '❤️', 'stickers/classic/03.webp', 2),
    ('cats-01',    'cats',    '🐱', 'stickers/cats/01.webp',    0),
    ('cats-02',    'cats',    '😹', 'stickers/cats/02.webp',    1);
