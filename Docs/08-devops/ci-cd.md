# CI/CD Pipeline

| Doc | Pipeline stages, gates, rollback |
|---|---|
| Status | v1.0 · Upstream: [/HLD.md](../../HLD.md) §19 · Cadence: trunk-based, deploy on green main, small batches |

## 1. Pipeline

```
PR ──► PR gates ──► merge to main ──► main build ──► ArgoCD auto-sync dev
                                            │            └─► staging (soak + synthetic load)
                                            └─ artifacts     └─► prod (Argo Rollouts canary, metric-gated)
```

## 2. PR gates (all required, < 10 min total)

| Gate | Tool |
|---|---|
| Lint | golangci-lint · eslint · buf lint |
| Protobuf breaking-change | buf breaking (against main) |
| Unit tests + race | `go test -race`, vitest |
| SAST | semgrep, gosec |
| Secrets scan | gitleaks |
| Dependency vulns | govulncheck, npm audit (fail on high) |
| Import-boundary check | custom lint: context port rules (core-api-lld §1) |
| Docs contract | endpoint added ⇒ API doc updated (path-based check) |

## 3. Main build

| Step | Contents |
|---|---|
| Integration tests | Testcontainers: PG + Valkey + NATS + MinIO; adapter suites |
| Protocol E2E | two headless clients through a real gateway: send/resume/dedupe/receipt scenarios ([test-strategy.md](../10-testing/test-strategy.md) §3) |
| Build | distroless multi-arch images, one per deployable; SBOM (syft) |
| Scan + sign | trivy (fail high) → cosign sign → push registry |
| Chart | helm package + values-schema validation |

Offline profile: same pipeline on self-hosted Gitea Actions/Woodpecker + Harbor (HLD §17.5); base images air-gap-mirrored.

## 4. CD & progressive delivery

- **dev:** ArgoCD auto-sync on merge.
- **staging:** auto-sync + 2 h soak with synthetic load (k6 profile); promotion manual-approved.
- **prod:** Argo Rollouts canary 10% → 50% → 100%, gated on Prometheus analysis (error rate, p95 latency, WS connect success); auto-abort + rollback on gate failure.
- DB migrations: expand–contract only; migration job runs as pre-sync hook; **contract phases deferred ≥ 1 release** so image rollbacks never fight schema (NFR-14).

## 5. Rollback

Image/config: GitOps revert (instant, declarative). Schema: never rolled back — contract-later discipline makes old images compatible. Feature regression: flag kill-switch first (faster than deploy), then revert.

## 6. Versioning & releases

Deployables: semver tags `core-api/v1.4.2`; images tagged sha + semver (never `latest`). Proto: append-only within `/v1`; breaking ⇒ new package version. Release notes generated from conventional commits ([git-workflow.md](../13-standards/git-workflow.md)).
