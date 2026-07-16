# ADR-005: Client-Side Content Search — No OpenSearch

Status: **Accepted** · Upstream: HLD correction #4, §14

## Context

Requirements ask for message/media search. Both the raw design and the 2026-07-16 proposal specified OpenSearch. Under E2EE the server holds ciphertext: **there is nothing to index server-side.** This is physics, not preference.

## Decision

- **Content search** (messages, media captions, files): client-side SQLite **FTS5** over the local decrypted store (BM25, prefix indexes; budget < 30 ms on 100k messages).
- **Metadata search** (usernames, group names/descriptions): PostgreSQL `pg_trgm` + FTS — data the server legitimately holds.

## Alternatives

- **OpenSearch:** an empty, expensive cluster (3+ JVM nodes) indexing nothing. Rejected.
- **Searchable encryption / blind indexes on content:** leaks query/frequency patterns, immature at product scale, still can't rank. Rejected for V2/V3.

## Consequences

- ✅ Search works offline; zero server cost; privacy preserved.
- ⚠️ Each device searches only its own synced history (inherent to E2EE; acceptable).
- Revisit trigger: none while E2EE-by-default stands.
