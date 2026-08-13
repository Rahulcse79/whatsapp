-- Admin console (T4.01, HLD §15.6, security-architecture §4). The admin plane
-- is gated by external OIDC SSO — the IdP owns admin membership and roles — so
-- an admin actor is an OIDC subject (an arbitrary string), NOT a platform user
-- uuid. Widen audit_log.actor to text to hold it. The append-only property and
-- the PUBLIC UPDATE/DELETE/TRUNCATE revoke from 000008 are unchanged.
ALTER TABLE audit_log ALTER COLUMN actor TYPE text USING actor::text;
