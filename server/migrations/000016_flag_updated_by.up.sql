-- Feature-flag management (T4.02, core-api-lld §5). Flags are written from the
-- admin plane, whose actors are OIDC subjects (arbitrary strings), not platform
-- user uuids — the same reasoning as 000015 for audit_log.actor. Widen
-- feature_flags.updated_by to text. Nothing deployed read the column as uuid
-- (feature_flags had no Go writer yet), so the widening is expand-safe.
ALTER TABLE feature_flags ALTER COLUMN updated_by TYPE text USING updated_by::text;
