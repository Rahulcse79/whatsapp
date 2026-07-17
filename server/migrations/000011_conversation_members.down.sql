DROP TABLE IF EXISTS conversation_members;
DROP INDEX IF EXISTS conversations_direct_key;
ALTER TABLE conversations DROP COLUMN IF EXISTS direct_key;
