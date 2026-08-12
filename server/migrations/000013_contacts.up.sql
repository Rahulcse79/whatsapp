-- Contacts context (T1.09): personal favorites + invite-a-friend capabilities.
-- Contract: Docs/04-api/auth-users-api.md §Contacts · threat-model-abuse.md T11/T12.
--
-- DEVIATION (see migrations/README.md): favorites are keyed by target user_id
-- here rather than the design-doc `contacts.favorite` boolean, because the
-- `PUT /favorites/{user_id}` API favorites a *user* (which may be a username-
-- search hit with no address-book edge), not a peppered-hash contact row.

CREATE TABLE favorites (
    owner_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    target_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, target_user_id)
);

-- Personal invite links (T12 capability tokens): unguessable, expiring,
-- max-uses, revocable. Distinct from the group-scoped `invite_links` table.
CREATE TABLE contact_invites (
    token      text PRIMARY KEY,                    -- unguessable capability token
    inviter_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    max_uses   integer NOT NULL DEFAULT 0,          -- 0 = unlimited
    uses       integer NOT NULL DEFAULT 0
);
-- "my invites" listing / revoke-by-owner.
CREATE INDEX contact_invites_by_inviter ON contact_invites (inviter_id);
