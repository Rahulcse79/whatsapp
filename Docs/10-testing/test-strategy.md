# Test Strategy

| Doc | Test pyramid, protocol tests, coverage policy, quality gates |
|---|---|
| Status | v1.0 · Tooling: `go test -race`, Testcontainers, vitest, Playwright, Maestro (RN), k6 |

## 1. Pyramid (with the WhatsApp-specific twist: protocol tests)

| Layer | Scope | Runs | Volume |
|---|---|---|---|
| Unit | `domain/` pure logic (state machines, windows, watermarks, GCRA math) | every PR, ms-fast | thousands |
| Adapter/integration | each adapter vs real PG/Valkey/NATS/MinIO (Testcontainers) | every PR | hundreds |
| **Protocol E2E** | two+ headless protobuf clients ↔ real gateway+core-api stack | main + nightly matrix | ~50 scenarios |
| Client E2E | Playwright (web PWA), Maestro (RN) on device farm | nightly | key journeys |
| Load/chaos/security | staging | pre-release + scheduled | [load-and-chaos-testing.md](load-and-chaos-testing.md) |

## 2. Coverage & gates

`domain/` ≥ 90% line+branch (it's pure — no excuse) · adapters ≥ 75% · overall gate: patch coverage may not decrease · mutation testing on crypto-adjacent and money-adjacent logic (windows, dedupe, fencing) quarterly. Coverage is a floor, not a goal — the protocol scenario list is the real spec.

## 3. Protocol E2E scenario catalog (the crown jewels — each maps to sequence-diagrams.md)

| # | Scenario | Asserts |
|---|---|---|
| P1 | send → deliver → receipts, both online | order, states, latency budget |
| P2 | send to offline → push dispatched → reconnect resume | exactly-once, cursor correctness |
| P3 | crash gateway mid-delivery (SIGKILL) | zero loss post-ACK, redelivery dedupe |
| P4 | duplicate MsgSend (network retry) | single seq assigned, one delivery |
| P5 | resume with stale/corrupt cursor | full-gap replay, no dupes client-side |
| P6 | group 1,024: send, member remove, key-rotation event ordering | rotation before next msg, aggregate receipts |
| P7 | edit inside/outside 15 m; delete inside/outside 48 h | window enforcement (server clock) |
| P8 | interleave: live msgs during replay | per-conv seq order preserved (gateway LLD §3) |
| P9 | blocked-user send / suspended-account send | correct rejects, no leakage via receipts |
| P10 | device revoke while connected | 4403 close, token invalid, prekeys gone |
| P11 | PTT: concurrent acquire ×10, speaker crash, zombie fence | FIFO fairness, ≤1 s failover, stale audio dead |
| P12 | call: offer/answer/decline/timeout matrix; answer-elsewhere | ring machine transitions |
| P13 | media: upload-interrupt-resume-complete; hash mismatch | resumability, reject path |
| P14 | server compromise simulation: capture all server-side bytes for a conversation | **no plaintext derivable** (fixture keys) |
| P15 | drain during 5k-conn load | zero loss, reconnect spread ≤ 120 s |

## 4. Client test focus

Web: outbox persistence across tab kill; PWA offline compose/read/search; IndexedDB/OPFS migration. Mobile: push wake → fetch → local notification (both platforms); CallKit/ConnectionService ring paths; background sync budget; SQLCipher migration between app versions. Crypto wrapper: cross-platform vector tests (same libsignal fixtures must interop web↔iOS↔Android).

## 5. Test data & environments

Fixture identity set (deterministic keys) for protocol tests — **never** production keys; probe accounts in prod are flagged + excluded from analytics. Staging seeded by generator (10k users, realistic group-size distribution: median 8, p99 512).

## 6. UAT (P4)

Scripted acceptance: every FR checked against its AC by a non-author; exploratory sessions on the personas ([user-stories-personas.md](../01-requirements/user-stories-personas.md)); accessibility audit (WCAG 2.2 AA — axe + manual screen-reader pass); offline-profile UAT runs the whole suite against a single-box deployment.
