-- Status content was unreachable for viewers (T5.11 defect).
--
-- GET /v1/stories/feed returned only {story_id, author, expires_at, key_available},
-- so a viewer had no way to locate the story's content: `kind` was never
-- persisted at all (it was treated as a post-time input only), and `media_ref`
-- was stored but never returned. The author's client cached the payload in local
-- storage, which meant a status was visible ONLY to its author, on the device
-- that posted it.
--
-- Persisting the kind lets the feed tell a viewer what to render, and returning
-- media_ref lets it fetch the ciphertext. Neither weakens E2EE: the blob is
-- encrypted with a per-story key that never reaches the server, and the feed is
-- already restricted to the audience snapshot.

ALTER TABLE stories ADD COLUMN IF NOT EXISTS kind smallint NOT NULL DEFAULT 2;

COMMENT ON COLUMN stories.kind IS '0 image | 1 video | 2 text (domain.Kind). Default 2 backfills pre-existing rows as text.';
