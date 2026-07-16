-- Extensions used by the schema:
--   citext  — case-insensitive unique usernames
--   pg_trgm — trigram search on usernames/group names (metadata search, ADR-005)
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
