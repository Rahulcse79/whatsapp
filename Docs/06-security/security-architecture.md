# Security Architecture

| Doc | AuthN/Z, transport, at-rest, secrets, supply chain, admin plane |
|---|---|
| Status | v1.0 · E2EE specifics: [e2ee-design.md](e2ee-design.md) · Threats: [threat-model-abuse.md](threat-model-abuse.md) |

## 1. Layered model

| Layer | Controls |
|---|---|
| Content | E2EE (libsignal) — server structurally cannot read chats/calls/stories/backups |
| Application | JWT auth, per-device identity, RBAC, rate limits, input validation, idempotency |
| Transport | TLS 1.3 only, HSTS, mobile cert pinning; mTLS inside the cluster |
| Data at rest | LUKS volumes (PG/MinIO/Valkey), SQLCipher on devices, hashed+peppered phone numbers |
| Infrastructure | Distroless non-root images, NetworkPolicies, K8s RBAC, cosign-signed images + SBOM |
| Process | Audit log, quarterly restore/security drills, external pentest before launch (P4) |

## 2. Authentication & sessions (FR-AUTH; HLD §15.2)

- Phone+OTP (or email/TOTP in offline profile) → device-bound token pair: access JWT **10 min** (EdDSA, kid-rotated keys) + rotating refresh token. Refresh reuse ⇒ kill session, alert all devices.
- 2FA registration PIN (Argon2id: m=64MB, t=3, p=4) blocks SIM-swap re-registration.
- Device revocation is atomic: tokens + prekeys + push route + WS close 4403 in one flow.
- Service-to-service: mTLS (cluster CA via cert-manager); per-deployable DB roles and NATS ACLs ([microservices.md](../02-architecture/microservices.md) §6).

## 3. Secrets & supply chain

| Item | Approach |
|---|---|
| Secrets | SOPS + age in Git; per-env keys; Vault when team > ~6; **no secrets in env-var manifests** (mounted files) |
| Key escrow | age keys held offline by 2 named holders (backup-recovery.md) |
| Images | Distroless, non-root, read-only rootfs; syft SBOM + trivy scan + cosign sign in CI; unsigned images rejected by admission policy |
| Dependencies | go.sum/lockfiles pinned; renovate PRs; `govulncheck` gate |
| Build | Hermetic builds in CI; no `latest` tags anywhere |

## 4. Admin plane (HLD §15.6 — narrowed by E2EE)

Separate SPA + hostname; OIDC SSO + hardware-key 2FA + IP allowlist; RBAC viewer → T&S → operator → owner; **every mutating action writes `audit_log` in the same transaction** (append-only — UPDATE/DELETE revoked at grant level). Admins can never read content: the data does not exist server-side. There is no support-staff "God mode" to abuse, phish, or subpoena.

## 5. Platform hardening checklist (P0 gate)

- [ ] NetworkPolicies: default-deny; explicit flows only (matrix in microservices.md §4)
- [ ] PodSecurity: restricted profile; no privileged pods (LiveKit host-network nodes documented exception, isolated node pool)
- [ ] Envoy: WAF rules, request size caps, slow-loris timeouts, per-IP GCRA
- [ ] PG: `pg_hba` scram-sha-256 + TLS; no superuser app roles; RLS not used (single-tenant schema, roles suffice)
- [ ] Valkey: AUTH + TLS; no KEYS/FLUSH commands for app roles (renamed)
- [ ] MinIO: per-bucket policies; presigned-only client access; no public buckets
- [ ] K8s: API server allowlisted; etcd encrypted at rest; no default SA tokens mounted

## 6. Privacy engineering (beyond E2EE)

- Data minimization is schema-enforced: no plaintext phone (HMAC+pepper), no content columns, no server logs of message metadata beyond routing needs; log schema bans PII fields ([monitoring doc](../09-observability/monitoring-logging-tracing.md)).
- Retention limits are jobs, not promises (HLD §7.5 table is binding).
- GDPR: export = client-driven (server contributes metadata it holds); delete = tombstone now, purge ≤ 30 d, media refcount cascade.
- Analytics: metadata-only aggregates; no per-user behavioral profiles (HLD §18.1).

## 7. Security testing cadence

SAST (semgrep, gosec) every PR · dependency audit every PR · secrets scan (gitleaks) every PR · DAST vs staging weekly · external pentest at P4 + annually · crypto review of the libsignal integration (not the primitives) before launch — details in [10-testing/load-and-chaos-testing.md](../10-testing/load-and-chaos-testing.md) §security.
