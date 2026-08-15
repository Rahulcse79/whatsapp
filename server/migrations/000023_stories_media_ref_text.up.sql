-- Fix: story media is E2EE-uploaded via the media pipeline, which yields an
-- object KEY (not a media_objects.id). The 000006 FK to media_objects made image
-- stories fail (object key isn't a uuid), and it was spurious anyway — stories
-- are decoupled from media refcounting (MinIO ILM + the 24 h purge are the
-- backstop). Store the object key as text.

ALTER TABLE stories DROP CONSTRAINT IF EXISTS stories_media_ref_fkey;
ALTER TABLE stories ALTER COLUMN media_ref TYPE text USING media_ref::text;
