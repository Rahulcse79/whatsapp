# Git Workflow

| Doc | Branching, commits, reviews, releases |
|---|---|
| Status | v1.0 · Model: **trunk-based development** — short-lived branches, deploy on green main (ci-cd.md) |

## Branching

- `main` is always deployable (dev auto-syncs from it). No develop branch, no gitflow.
- Branches: `feat/T0.11-chat-accept`, `fix/inbox-replay-order`, `docs/adr-007` — named by task ID where applicable; lifetime target < 2 days (slice tasks smaller instead of long branches).
- No direct pushes to main (⚙ protected); PRs require: green gates + 1 review (2 for `internal/chat`, crypto-wrapper, migrations).
- Long-running work: feature flags + incremental merges — **not** long branches.

## Commits (conventional, ⚙ commitlint)

```
feat(chat): assign per-conversation seq in accept tx
fix(gateway): buffer live frames during replay per conversation
docs(adr): add ADR-007 …
perf|refactor|test|build|ci|chore(scope): …
```
Body: the why + doc/task links. Breaking proto/API change: `!` + migration note (rare by policy — append-only proto).

## Releases

- Continuous to dev/staging from main; prod = promoting a staged version via GitOps PR (ArgoCD app targets a tag).
- Tags per deployable: `core-api/v1.4.2` (independent cadence when needed, usually lockstep early).
- Release notes auto-generated from conventional commits; DB contract-phase notes highlighted (ci-cd.md §4 discipline).
- Hotfix: branch from the deployed tag only if main has moved unreleasably; otherwise fix-forward through the same pipeline (preferred, canary makes it safe).

## Migrations & rollback etiquette

Schema PRs: expand-only; the contract PR references the expand PR and waits ≥ 1 release. Revert = `git revert` PR through the pipeline (GitOps revert for emergencies, then reconcile main). Never `--force` on shared branches; `main` history is append-only.
