# Roadmap & Milestones

| Doc | Delivery phases P0–P4, exit criteria, V3 backlog |
|---|---|
| Status | v1.0 · Upstream: [/HLD.md](../../HLD.md) §23 · Tasks: [task-breakdown.md](task-breakdown.md) · Epic map: HLD Appendix D |

## Phases

| Phase | Duration | Scope | **Exit criteria (binding)** |
|---|---|---|---|
| **P0 — Foundations** | 6–8 wks | Monorepo+CI/CD, K8s+GitOps, observability, proto contracts, auth+devices+keys, E2EE 1:1 chat, delivery states, presence/typing, push, sync/resume | Two phones exchange E2EE messages through prod-shaped infra; **chaos-kill a gateway with zero message loss** (protocol test P3 green in staging) |
| **P1 — Groups & media** | 4–6 wks | Groups+Sender Keys, media pipeline, overlays (edit/delete/reactions/pins), contact sync, client search | 1,024-member group + 25 MB resumable media pass load test; key rotation verified (P6) |
| **P2 — Calling** | 4–6 wks | LiveKit+coturn, 1:1 voice/video, CallKit/ConnectionService, group calls ≤ 32, screen share | p95 setup ≤ 3 s; locked-phone ringing on both platforms |
| **P3 — Multi-device, PTT, stories** | 4–6 wks | Device linking+history bootstrap, PTT floor control, stories, encrypted backups | PTT p95 grant ≤ 200 ms @ 200 listeners; link/revoke flows pass P10 |
| **P4 — Hardening & launch** | 3–4 wks | Admin console+T&S, analytics rollups, 20k load (3× burst), security audit+pentest, DR drill, runbooks, UAT | **SLOs green 2 consecutive weeks under synthetic load with chaos enabled** |

## Milestone gates (between phases)

Gate review checks: exit criteria demonstrated live (not slideware) · no P0/P1 bugs open · docs updated (any drift doc↔code fails the gate) · error budget healthy · threat-model rows added for new features.

## V3 backlog (post-launch, ordered by expected value)

1. Tauri desktop shell · 2. Broadcast channels/communities (own bounded context — HLD §2.3) · 3. CDN for media GETs · 4. MLS for mega-groups · 5. On-device AI (14-ai doc) · 6. PSI contact discovery · 7. Streaming PTT tier (5k+ listeners) · 8. Progressive video playback · 9. Sealed-sender study · 10. Multi-region

## Standing risks tracked at every gate (HLD §24)

libsignal AGPL legal posture · SMS spend/provider (P0 decision) · iOS review constraints (VoIP push) · media-plane on-call reality (LiveKit Cloud escape hatch) · "20k concurrent vs total" assumption (affects node counts only) · data-residency requirements of the eventual user base.
