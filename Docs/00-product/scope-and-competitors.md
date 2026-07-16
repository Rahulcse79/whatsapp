# Scope & Competitor Analysis

| Doc | Scope boundaries + competitive positioning |
|---|---|
| Status | v1.0 |
| Upstream | [/HLD.md](../../HLD.md) §2.1–2.3, Appendix D |

## 1. Scope tiers

| Tier | Contents |
|---|---|
| **MVP (P0–P1)** | Auth + devices + E2EE 1:1 chat + delivery states + presence + push + groups + media + contacts |
| **V2 (P2–P4)** | Voice/video calls ≤ 32, screen share, PTT, stories, multi-device, encrypted backups, admin console, analytics, launch hardening |
| **V3 (backlog)** | Tauri desktop, MLS for mega-groups, PSI contact discovery, broadcast channels/communities, streaming PTT tier, CDN media, progressive video playback, on-device AI, multi-region |

## 2. Non-goals for V2 (decided — HLD §2.3)

- Federation/interop, business/bots API, payments.
- Multi-region active-active.
- Server-side message archive or content analytics (impossible under E2EE anyway).
- Native desktop apps (PWA first).
- Broadcast channels/communities (different product shape; V3 as its own bounded context).
- AI features (on-device only under E2EE; V3).

## 3. Competitor analysis

| | WhatsApp | Signal | Telegram | Discord | **This product** |
|---|---|---|---|---|---|
| E2EE default | ✅ (Signal protocol) | ✅ (reference) | ❌ (opt-in "secret chats" only) | ❌ | ✅ (libsignal) |
| Self-hostable | ❌ | ❌ (server OSS but not supported) | ❌ | ❌ | ✅ **differentiator** |
| Server-side history | ❌ (relay) | ❌ (relay) | ✅ (cloud chats) | ✅ | ❌ (relay — deliberate) |
| Group size | 1,024 | 1,000 | 200,000 | huge | 1,024 (MLS for larger in V3) |
| Calls | ≤ 32 | ≤ 50 | ≤ 1k viewers | good | ≤ 32 + PTT (differentiator) |
| Offline/air-gap deploy | ❌ | ❌ | ❌ | ❌ | ✅ **differentiator** |
| Multi-device | ✅ | ✅ | ✅ | ✅ | ✅ (1 primary + 4 linked) |

**Positioning:** Telegram wins on features by keeping plaintext server-side — we refuse that trade. Signal wins on trust but can't be self-hosted meaningfully. Our wedge: **Signal-grade privacy + WhatsApp-grade product + self-hosting** (including fully offline), plus PTT which none of the four treat as first-class.

## 4. Scope-change protocol

Any scope addition requires: (1) an entry in [12-planning/epics.md](../12-planning/epics.md), (2) an E2EE-feasibility check (can it work when the server sees only ciphertext?), (3) an HLD delta if it touches architecture. Features that fail the E2EE check are redesigned client-side or rejected — precedent: link previews, search, thumbnails (HLD corrections #4, #6; §2.1).
