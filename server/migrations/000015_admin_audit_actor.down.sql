-- Revert audit_log.actor to uuid. Assumes every actor value is a valid uuid
-- (true only if no OIDC-subject rows were written); non-uuid rows fail the cast.
ALTER TABLE audit_log ALTER COLUMN actor TYPE uuid USING actor::uuid;
