# Database migrations

Plain-SQL migrations in [golang-migrate](https://github.com/golang-migrate/migrate)
format (`NNNNNN_name.up.sql` / `.down.sql`). Schema contract:
[Docs/03-database/database-design.md](../../Docs/03-database/database-design.md).

## Rules (binding — Docs/08-devops/ci-cd.md §4)

1. **Expand–contract only.** A migration may add; it may never rename/drop
   anything the currently-deployed release still reads. Contract migrations
   land ≥ 1 release after the code stopped using the old shape.
2. Down migrations must genuinely revert — CI applies `up`, `down -all`,
   then `up` again against PostgreSQL 17 on every commit.
3. Any statement against `message_inbox` needs an EXPLAIN note in the PR.
4. DB **roles/grants are not migrations** — they're per-environment bootstrap
   (deploy/, task T0.22): `core_api_rw`, `media_rw`, `notify_rw`, `admin_ro`
   + `audit_append`. `audit_log` gets INSERT-only for every app role.

## Deliberate deviations from the design doc (kept honest here)

- **No covering `INCLUDE (ciphertext)` index on `message_inbox`** — ciphertext
  is up to 256 KB; oversized index tuples would fail. The PK
  `(recipient_device_id, conversation_id, seq, msg_uuid)` serves the replay
  scan; the heap fetch touches hot pages.
- `message_inbox` is HASH-partitioned ×16 only; monthly RANGE sub-partitioning
  (for `DROP PARTITION` TTL purges) arrives with pg_partman when the delete
  job's cost shows up in metrics — expand-contract makes that a later ADD.
- `accepted_at` column added (needed for overlay-window validation and
  `InboxItem.accepted_at_ms`); doc update pending.
- **`favorites` table (000013)** keys favorites by `target_user_id` instead of
  the design doc's `contacts.favorite` boolean. `PUT /favorites/{user_id}`
  favorites a *user* — which may be a username-search hit with no address-book
  edge — so the phone-hash-keyed `contacts` row can't represent it. The
  `contacts.favorite` column is now unused (kept for expand-contract; a later
  contract migration may drop it).
- **`contact_invites` table (000013)** holds personal invite-a-friend
  capability tokens (T1.09), distinct from the group-scoped `invite_links`.
- **`audit_log.actor` widened uuid → text (000015)** — the admin plane (T4.01)
  is gated by external OIDC SSO, so an admin actor is an IdP subject (an
  arbitrary string), not a platform user uuid. Nothing deployed read the column
  as uuid yet (admin was a scaffold), so the widening is expand-safe. The
  append-only property (PUBLIC UPDATE/DELETE/TRUNCATE revoked in 000008) is
  unchanged; `target` stays uuid (report/user ids).

## Local usage (optional — CI is the authority)

```
migrate -path server/migrations -database "$WA_PG_DSN" up
```
