# Go-Live Checklist & Staged Launch

| Doc | The final gate before production, the version-tag flow, and the staged rollout plan |
|---|---|
| Status | v1.0 · Launch gate (roadmap-milestones §P4): **SLOs green 2 consecutive weeks under synthetic load with chaos enabled** |
| Rule | Every box is demonstrated **live, not slideware** (gate-review rule). Any doc↔code drift fails the gate. |

## 1. Phase gates — all must be green

Each phase has a binding exit gate; production waits on all five. Gates run
against **staging** with the real stack (they are the infra-gated items called
out through the build).

- [ ] **GATE P0** — chaos-kill a gateway during the protocol tests ⇒ **zero
  message loss** (P3 scenario green in staging). *Slice green in CI; full run
  needs Docker/testcontainers + staging.*
- [ ] **GATE P1** — 1,024-member group send + 25 MB resumable upload pass the CI
  load job (`ops/loadtest/fanout.js`; correctness = scenario P6/P13). *Unit
  slices green in CI; load-job run needs staging.*
- [ ] **GATE P2** — call setup p95 ≤ 3 s in staging (`ops/loadtest/callsurge.js`);
  locked-phone ring on both platforms (T2.04 native modules wired). *Unit slices
  green; staging + on-device run pending.*
- [ ] **GATE P3** — PTT floor-grant p95 ≤ 200 ms @ 200 listeners
  (`ops/loadtest/ptt.js`); device link/revoke flows clean. *Slices green;
  staging run pending.*
- [ ] **GATE P4 / launch** — the sustained profile (`ops/loadtest/sustained.js`)
  green for **two consecutive weeks** with the `ops/chaos/` scenarios enabled and
  the durability audit (`msgs_lost==0`) holding throughout.

## 2. Readiness checklist (demonstrated live)

| Area | Requirement | Evidence |
|---|---|---|
| Security | External pentest booked + all findings remediated (T4.06); SAST/deps/secrets green every PR; crypto-integration review done | pentest report; CI security jobs |
| DR | Game day run (T4.07): restore from Git + backups within RTO ≤ 60 min, RPO proven; backup restore drill logged | `ops/chaos/game-day.yaml` |
| Durability | Zero-loss audit (NFR-12) green across every load + chaos profile | `ops/loadtest/auditor.js` output |
| Chaos | Always-on lite scenarios running in staging; game-day scenarios exercised | `ops/chaos/` |
| Observability | 7 dashboards live; every paging alert maps to a runbook (T4.08); synthetic probe green | `ops/dashboards`, `ops/alerts`, `ops/runbooks` |
| On-call | Rotation staffed; escalation policy live; pager tested end-to-end | `ops/runbooks/README.md` |
| UAT | FR×AC matrix signed off (T4.09); axe/Lighthouse clean; a11y device pass | `Docs/01-requirements/uat-matrix.md` |
| Offline | Self-hosted profile validated end-to-end (T4.10) | `values-offline.yaml`, compose `--profile offline` |
| Data | Migrations expand-only; contract phases deferred ≥ 1 release; PgBouncer pooling on | `server/migrations/`, ci-cd §4 |
| Abuse | Rate limits bind under load; kill-switches tested; OTP spend cap set | flag console, `ops/loadtest` load-time authz |
| Legal/keys | libsignal AGPL posture confirmed; secrets in SOPS/KMS, not repo; CA/root secured | HLD §24 |
| Docs | No doc↔code drift (gate rule); threat-model rows added for new features | docs-contract CI gate |

## 3. Version & release-tag flow

SemVer, single `VERSION` file, trunk-based (git-workflow.md). A release is a tag,
not a branch:

```bash
# 1) bump VERSION on main via PR (green gates + review)
echo 1.0.0 > VERSION            # commit "chore(release): v1.0.0"
# 2) tag it — release.yml fires on `v*`
git tag v1.0.0 && git push origin v1.0.0
```

- `release.yml` (on `push: tags: v*`) builds version-stamped backend binaries
  (linux amd64/arm64, `-ldflags`), the Android APK + iOS archive, and a GitHub
  Release with `whatsapp-v2-<component>-<version>-<os>-<arch>` artifacts.
- App versioning: `versionName` = the tag; `versionCode` / build number = the
  GitHub run number (monotonic).
- Per-deployable tags (`core-api/v1.4.2`) are available for independent cadence;
  early releases move in lockstep.

## 4. Staged rollout (GitOps + Argo Rollouts)

Promotion is a Git operation; nothing is hand-deployed (ci-cd.md §3):

```
merge to main ──► ArgoCD auto-sync ──► dev
                                         │  (bake + synthetic load)
   GitOps PR: point wa-platform-staging at the tag ──► staging
                                         │  (soak + synthetic load, chaos-lite on)
   GitOps PR: point wa-platform-prod at the tag ──► prod (Argo Rollouts canary)
```

**Prod canary** (Argo Rollouts, metric-gated on Prometheus analysis — error rate,
p95 latency, WS connect success):

1. **10%** — hold, analysis must pass (auto-abort + rollback on breach).
2. **50%** — hold, analysis pass.
3. **100%** — full.

Migration job runs as a **pre-sync hook**; expand–contract means an image
rollback never fights the schema (NFR-14). Rollback = GitOps revert (instant
image rollback) for emergencies, then reconcile `main`.

## 5. Staged user launch (feature-flag gated)

The infra rollout above is orthogonal to the **audience** ramp, which rides the
flag/kill-switch console (no deploy):

1. **Internal / dogfood** — team + allowlist; watch dashboards for a week.
2. **Invite-only beta** — capped signups (OTP + new-account rate limits already
   graduate over 7 days, threat-model §3); collect UAT + crash-free ratio.
3. **Percentage GA** — flag-driven 5% → 25% → 100% of new registrations;
   kill-switch pauses registration instantly if anything smokes.
4. **General availability** — flags flipped default-on; announce.

## 6. Launch-day runbook

- **T-1 day:** freeze non-critical merges; confirm every §2 box; page-test the
  on-call chain; verify the last staged tag is the one being promoted.
- **Go / no-go:** all gates green + error budget healthy + no open P0/P1 bugs.
- **Cut:** tag → promote to prod via the canary above; watch the analysis gates.
- **Rollback trigger:** any canary analysis breach, a durability-audit gap, or a
  paging alert with no fast mitigation → GitOps revert to the prior tag (instant),
  then a blameless [postmortem](../../ops/runbooks/postmortem-template.md).
- **Comms:** status page + a rollback/known-issues channel staffed through the
  ramp.

## 7. Post-launch

- Hold the sustained profile + chaos for the **2-week** SLO window (the P4 gate);
  the durability audit must stay at zero loss the whole time.
- Track the error budget; a burn pauses the audience ramp, not just alerts.
- Reconcile any hotfixes back to `main` (fix-forward preferred; the canary makes
  it safe).
- When every gate has held for two weeks, the project is **production-launched**
  (the last line of `Docs/TASKS.txt`).
