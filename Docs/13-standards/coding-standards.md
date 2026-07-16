# Coding Standards

| Doc | Go + TypeScript standards; lint-enforced where possible |
|---|---|
| Status | v1.0 · Principle: rules that matter are enforced by tools, not memory. Anything below marked ⚙ is a CI gate. |

## Go (server)

- **Version/layout:** Go 1.24+; `server/cmd/<deployable>/main.go` wiring-only; contexts under `server/internal/<ctx>/{domain,adapters,port.go}` (core-api-lld §1).
- ⚙ **Import boundaries:** contexts↔contexts via `port.go` only; `domain/` imports no I/O; enforced by depguard/custom lint.
- ⚙ golangci-lint config: errcheck, govet, staticcheck, gosec, exhaustive (state-machine switches must be exhaustive), sqlclosecheck, bodyclose.
- **Errors:** wrap once with context (`fmt.Errorf("accepting message: %w", err)`); sentinel errors per port (`ErrDuplicate`, `ErrWindowClosed`); log once at outermost handler (design-patterns §5). No panics past init.
- **Concurrency:** every goroutine has an owner + shutdown path (context cancellation); channels have documented capacity rationale; ⚙ `-race` always in CI; no `time.Sleep` synchronization in prod code or tests.
- **DB:** sqlc-style generated queries; no string-built SQL; every `message_inbox` query needs an EXPLAIN review note in the PR.
- **Naming/docs:** package comments required; exported symbols documented; table-driven tests; test names state behavior (`TestAccept_DuplicateUUID_ReturnsPriorAck`).
- Protobuf: buf-generated only; ⚙ never hand-edit `gen/`; append-only fields; reserved on removal.

## TypeScript (clients + shared packages)

- ⚙ `strict: true`, `noUncheckedIndexedAccess`; no `any` (eslint error, `unknown` + narrowing instead).
- **State discipline:** UI derives from local DB / stores only; network mutates DB, never components (offline-sync doc §2 invariant). TanStack Query for REST cache; Zustand stores small and serializable.
- **Shared packages** (`clients/packages/`): no React imports (framework-free domain code); platform adapters implement package interfaces.
- ⚙ eslint: exhaustive-deps, no-floating-promises, import/no-cycle.
- Crypto rule: **only** the crypto-wrapper package touches libsignal; UI code never sees key material types.

## Both

- ⚙ Conventional commits (feed release notes) — [git-workflow.md](git-workflow.md).
- Feature flags for anything user-visible and risky; flag cleanup ≤ 2 releases after 100%.
- No TODOs without issue links (⚙ lint).
- Comments explain **why** (constraints, invariants), never what the next line does.
- PR template: task ID, docs links, test evidence, EXPLAIN notes (if hot-path SQL), threat-model rows (if new surface).

## Review standards

Reviewer checks in order: correctness vs linked doc → tests actually assert the behavior → error/retry paths → observability (new failure mode ⇒ new metric/log?) → boundaries respected. Style nits are the linter's job, not the reviewer's.
