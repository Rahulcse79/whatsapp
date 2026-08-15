-- Revert media_ref to a uuid FK. Safe on a fresh/round-trip DB (no rows); with
-- real object-key data the cast would fail, which is the point — don't roll back
-- past object-key stories.
ALTER TABLE stories ALTER COLUMN media_ref TYPE uuid USING media_ref::uuid;
ALTER TABLE stories ADD CONSTRAINT stories_media_ref_fkey
    FOREIGN KEY (media_ref) REFERENCES media_objects (id) ON DELETE SET NULL;
