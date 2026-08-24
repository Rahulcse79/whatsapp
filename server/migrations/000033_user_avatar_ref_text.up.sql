-- Profile pictures (users.avatar_ref).
--
-- The column has existed unused since 000002 with a uuid FK to media_objects
-- added in 000003. That FK is the same defect migration 000023 fixed for
-- stories: the media pipeline yields an object KEY (text), not a
-- media_objects.id, so setting avatar_ref from a real upload fails on the cast.
--
-- Store the object key as text, as stories now do. An avatar is decoupled from
-- media refcounting the same way: it is identity metadata with a single owner,
-- replaced wholesale on change, and MinIO lifecycle is the backstop.
--
-- NOTE: groups.avatar_ref (migration 000004) still carries the identical uuid
-- FK and the same latent bug; it is left alone here rather than changed as a
-- side effect, and is tracked as a follow-up.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_avatar_ref_fkey;
ALTER TABLE users ALTER COLUMN avatar_ref TYPE text USING avatar_ref::text;

COMMENT ON COLUMN users.avatar_ref IS 'Object key of the profile picture in MinIO, or NULL. Plaintext: an avatar is identity metadata (like display_name), not message content.';
