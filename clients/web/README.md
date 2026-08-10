# web/ — React 19 + Vite PWA (`@wa/web`)

Built at task **T0.19** (shell): same features as the mobile shell (OTP auth, chat
list + thread), reusing the shared **`@wa/client-core`** engine (WS resume state
machine, OTP client, `MemoryMessageRepo`, `planInboxBatch`).

Layering follows [web-app-architecture.md](../../Docs/11-clients/web-app-architecture.md) §1:

- **Dedicated worker** (`src/worker/db.worker.ts`) — the local DB + crypto, off the
  main thread. Hosts `MemoryMessageRepo` (OPFS SQLite-wasm is the persistence epic)
  and `MockSessionCipher` (real E2EE at T0.20), reached via a typed RPC (`src/worker/rpc.ts`).
- **Main thread** (`src/services/appServices.ts`) — the `WsClient` connection +
  `SessionManager` + `OtpClient`, holding a cursor mirror so `Hello.last_cursors`
  stays synchronous while the store lives in the worker.
- **Platform adapters** (`src/platform/`) — `fetch`, `WebSocket`, timers, and an
  **IndexedDB** SecureStore (no secrets in localStorage, §3).
- **Service worker + manifest** — `vite-plugin-pwa` (Workbox) precaches the app
  shell for offline and makes the app installable. **WebPush** registration in
  `src/push.ts` (guarded; VAPID key via `VITE_VAPID_PUBLIC_KEY`).
- **UI** — React 19, a small view-state router (`src/App.tsx`) over the screens in
  `src/ui/screens.tsx`.

## Commands (CI runs typecheck + test; nothing is built locally by policy)

```bash
pnpm --filter @wa/web typecheck   # tsc -p .
pnpm --filter @wa/web dev         # vite dev server
pnpm --filter @wa/web build       # vite build (emits SW + manifest)
```

PWA installability + offline are a manual/Lighthouse check against a `vite build`
served over HTTPS/localhost.
