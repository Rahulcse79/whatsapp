# Disaster Recovery

| Doc | DR objectives, scenarios, drills |
|---|---|
| Status | v1.0 · Data-tier detail: [03-database/backup-recovery.md](../03-database/backup-recovery.md) |

## 1. Objectives

| Metric | Target |
|---|---|
| RPO (PG) | ≤ 5 min (continuous WAL) |
| RPO (media) | ≤ 24 h (daily mirror); backups bucket ≤ 1 h |
| RTO (full cluster loss) | ≤ 60 min (Terraform + GitOps + backup restore) |
| User-perceived message loss | **zero** for server-ACKed messages (client outbox + dedupe absorb the RPO window — see backup-recovery.md §3) |

## 2. Scenario playbook

| Scenario | Response | Expected impact |
|---|---|---|
| Pod/node loss | K8s reschedules; routes/consumers self-heal | seconds; none visible |
| PG primary loss | CloudNativePG auto-promote replica (~30 s); PgBouncer repoints | <1 min write errors; outbox retries mask it |
| Valkey quorum loss | Sentinel promote ≤ 10 s; keys rebuild by design | presence/typing blank briefly; sends fail-closed seconds |
| NATS quorum loss | RAFT recovers on 2/3; full loss: streams re-created from chart — inbox replay heals delivery | delivery lag, no loss |
| MinIO node loss | EC 2+2 serves degraded; replace node, heal | slower writes |
| Full cluster/site loss | Terraform rebuild → ArgoCD sync → PG PITR from off-site WAL → MinIO restore from mirror → DNS cutover | RTO ≤ 60 min; RPO per table above |
| Ransomware/compromised cluster | Rebuild from Git (declarative) + **immutable/versioned** backup buckets (object-lock on WAL archive); rotate all secrets (SOPS re-key), revoke cluster CA | RTO day-scale; the drill that matters most |
| Region unavailable (provider) | Standby site: restore-based (not active-active) per HLD §16.3 region model | RTO ≤ 60 min if pre-provisioned |

## 3. What makes this work (design dependencies)

Stateless services (any pod anywhere) · GitOps = infrastructure is a repo · relay model = tiny durable state (≤ ~50 GB PG + media) · client outbox = user-level RPO ≈ 0 · Valkey/NATS hold nothing durable.

## 4. Drill calendar (results logged in ops/runbooks/drills/)

| Cadence | Drill |
|---|---|
| Quarterly | PITR restore to scratch + automated validation |
| Quarterly (alt) | Site-loss game day: rebuild from repo+backups, stopwatch vs RTO |
| Semi-annual | Secrets-compromise rotation drill |
| Every deploy | Chaos-kill one ws-gateway during synthetic load (zero-loss assertion in CI staging) |

**Rule:** a backup that isn't restore-tested doesn't exist; a runbook that hasn't been executed is fiction.
