# Web App Architecture — React + Vite PWA

| Doc | Web client design (desktop-class experience; Tauri shell V3) |
|---|---|
| Status | v1.0 · Stack: React 19 + Vite + TS strict; TanStack Query; Zustand; ADR-006 (no Next.js — authed realtime SPA) |

## 1. Structure

```
app shell (routes, panes: chat-list | conversation | details)
├─ state: Zustand (realtime UI) + TanStack Query (REST) — same split as mobile
├─ shared domain packages (clients/packages/): sync-engine · crypto-wrapper · proto-types
├─ web platform adapters:
│    storage: SQLite-wasm on OPFS (SQLCipher-equivalent via encrypted VFS)
│    crypto: libsignal WASM build; WebCrypto non-extractable wrapping keys
│    push: WebPush (VAPID); media: File System Access API; calls: LiveKit JS SDK
└─ workers:
     dedicated worker: DB + crypto (keeps main thread at 60 fps)
     service worker: PWA cache, push events, offline shell
```

## 2. PWA behavior

Install prompt after engagement; offline = full shell + local data (read/compose/search); background sync API for outbox flush; WebPush notifications with local decrypt-and-render (same never-plaintext rule). Storage persistence API requested (`navigator.storage.persist()`) — eviction warning UX if denied.

## 3. Web-specific security posture

Weakest key-storage platform (e2ee-design §9): identity keys wrapped by WebCrypto non-extractable keys; strict CSP (no inline, no eval — WASM exempted via hashes); session bound to browser profile; "linked device" semantics: revocable from phone, auto-expires after 30 d inactivity (configurable). No secrets in localStorage — IndexedDB + wrapped keys only.

## 4. Performance budgets

First load (cold, cache-miss) ≤ 300 KB critical JS (code-split: calls/media-viewer/settings lazy) · route-change ≤ 100 ms · message render 60 fps with virtualization (chat-list + conversation virtual scrolling) · search-as-you-type < 50 ms (FTS in worker).

## 5. Accessibility (NFR-25 — release gate)

WCAG 2.2 AA: full keyboard navigation (roving tabindex in lists), screen-reader message semantics (live regions for incoming), focus management on pane switches, prefers-reduced-motion, contrast tokens both themes; axe CI + manual SR pass per release (test-strategy §6).
