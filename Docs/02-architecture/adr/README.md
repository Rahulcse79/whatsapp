# Architecture Decision Records

Decisions are immutable once **Accepted**; to change one, write a new ADR that supersedes it. Template at the bottom.

| ADR | Decision | Status |
|---|---|---|
| [ADR-001](ADR-001-relay-model.md) | Relay model — no server-side message archive | Accepted |
| [ADR-002](ADR-002-go-backend.md) | Go backend (Java/Spring rejected) | Accepted |
| [ADR-003](ADR-003-nats-over-kafka.md) | NATS JetStream over Kafka | Accepted |
| [ADR-004](ADR-004-livekit-sfu.md) | LiveKit SFU — operate, don't build | Accepted |
| [ADR-005](ADR-005-client-side-search.md) | Client-side content search; no OpenSearch | Accepted |
| [ADR-006](ADR-006-client-stack.md) | React Native + Vite PWA (Flutter/Next.js rejected) | Accepted |

## Template

```markdown
# ADR-NNN: <decision>
Status: Proposed | Accepted | Superseded by ADR-MMM
Date: YYYY-MM-DD
## Context      — the forces at play, the requirement driving this
## Decision     — one paragraph, active voice
## Alternatives — each with the concrete reason it lost
## Consequences — good, bad, and the revisit trigger
```
