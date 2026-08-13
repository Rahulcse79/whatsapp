-- Revert feature_flags.updated_by to uuid. Assumes every value is a valid uuid
-- (true only if no OIDC-subject rows were written); non-uuid rows fail the cast.
ALTER TABLE feature_flags ALTER COLUMN updated_by TYPE uuid USING updated_by::uuid;
