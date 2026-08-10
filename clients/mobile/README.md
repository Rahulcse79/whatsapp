# mobile/ — React Native + Expo app (`@wa/mobile`)

Built at task **T0.18** (shell): OTP auth, chat list + thread UI, a SQLite local
store wired to `@wa/sync-engine`, and a WS client with reconnect/backoff/resume.

Layering mirrors [mobile-app-architecture.md](../../Docs/11-clients/mobile-app-architecture.md) §1:

- **`src/core/`** — framework-free, no React Native imports, unit-tested in Node.
  Wire frames + ports, the WS connection/resume state machine (`wsClient.ts`),
  full-jitter backoff, the OTP/refresh REST client, and the SQLite-backed stores
  (`SqliteOutboxStore` implements `@wa/sync-engine`'s `OutboxStore`; `planInboxBatch`
  is the pure inbox-merge). The wire codec is JSON in this shell — binary protobuf
  via `@wa/proto-types` lands with E2EE wiring (T0.20).
- **`src/platform/`** — thin adapters implementing the core ports over Expo:
  `expo-sqlite`, `expo-secure-store`, `fetch`, `WebSocket`, real timers.
- **`src/services/`, `src/ui/`, `app/`** — `AppServices` assembly, the React
  context, and the expo-router screens (`/login`, `/verify`, `/chats`, `/thread/[id]`).

`app.config.ts` stamps versionName/versionCode from the release job's env and, by
existing, activates the Android APK + iOS archive jobs in `.github/workflows/release.yml`.

## Commands (CI runs these; nothing is built locally by policy)

```bash
pnpm --filter @wa/mobile typecheck   # tsc -p .
pnpm --filter @wa/mobile test        # vitest (core only)
```

Native builds happen on a version tag via `expo prebuild` in release.yml.
