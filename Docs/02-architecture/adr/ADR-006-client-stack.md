# ADR-006: Client Stack — React Native + React/Vite PWA

Status: **Accepted** (reaffirmed 2026-07-16 against Flutter + Next.js proposal) · Upstream: HLD §1.1, correction #7

## Context

One small team must ship iOS, Android, web, and (V3) desktop. The 2026-07-16 proposal recommended Flutter (mobile) and Next.js (web).

## Decision

- **Mobile: React Native + Expo** — CallKit/ConnectionService integrations, OTA updates, one JS/TS talent pool with web.
- **Web: React 19 + Vite PWA** (TanStack Query, Zustand) — installable, offline-first; Tauri desktop shell in V3.

## Alternatives

- **Flutter:** genuinely viable (performance, WebRTC support); loses on talent-pool unification with the web client and web-code sharing (crypto wrapper, sync engine, proto types are shared TS packages). *Team-shape decision, not a correction.* Rejected.
- **Next.js:** SSR/SEO machinery for an authed, stateful, offline-capable realtime app is dead weight; a marketing site (if ever) can use it separately. Rejected.

## Consequences

- ✅ `clients/packages/*` shared across web+mobile: proto types, libsignal wrapper, sync engine.
- ⚠️ RN native-module work at the call/push edges (CallKit, ConnectionService, keystore) — budgeted in epics.
- Revisit trigger: sustained RN pain at the media/call layer → re-evaluate per-platform native.
