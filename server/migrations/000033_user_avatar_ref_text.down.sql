-- Reverting to uuid discards any non-uuid object key, which is the whole point
-- of the forward migration; NULL those rows rather than fail the down.
UPDATE users SET avatar_ref = NULL
 WHERE avatar_ref IS NOT NULL
   AND avatar_ref !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';
ALTER TABLE users ALTER COLUMN avatar_ref TYPE uuid USING avatar_ref::uuid;
ALTER TABLE users
    ADD CONSTRAINT users_avatar_ref_fkey
    FOREIGN KEY (avatar_ref) REFERENCES media_objects (id) ON DELETE SET NULL;
