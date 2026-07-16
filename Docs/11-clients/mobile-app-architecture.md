# Mobile App Architecture — React Native + Expo

| Doc | Mobile client design (iOS + Android) |
|---|---|
| Status | v1.0 · Stack: RN + Expo (ADR-006); TypeScript strict; shared packages with web |

## 1. Layered architecture

```
UI (screens, navigation — expo-router)
├─ state: Zustand stores (chat list, session) + TanStack Query (server REST state)
├─ domain services (TS, shared w/ web via clients/packages/):
│    sync-engine (outbox, cursors, replay merge) · crypto-wrapper (libsignal bindings)
│    proto-types (buf-generated) · media-pipeline (compress/encrypt/chunk)
├─ platform adapters (RN-specific):
│    SQLCipher (op-sqlite) · keystore (Secure Enclave/Android Keystore)
│    push (FCM/APNs/ntfy driver) · CallKit / ConnectionService · file/camera
└─ transport: WS client (reconnect/backoff/resume) · REST client · LiveKit RN SDK
```

Rule mirroring the server: domain services import no RN modules (testable in Node); platform adapters implement interfaces from `clients/packages/`.

## 2. Local database (SQLCipher — schema in [offline-sync-local-store.md](offline-sync-local-store.md))

Key at rest in hardware keystore; DB opened after biometric/PIN app-lock (optional). Migrations versioned + tested across app versions (test-strategy §4).

## 3. The four hard platform integrations (budgeted in epics)

| Integration | iOS | Android |
|---|---|---|
| Message push wake | Notification Service Extension: data push → fetch via resume → decrypt → local notif (content never in push) | FCM data msg → headless task fetch/decrypt/notify; ntfy conn in offline profile |
| Call ring locked | PushKit VoIP → CallKit (report immediately — iOS kills apps that don't) | high-prio FCM → ConnectionService full-screen intent |
| Background sync | BGAppRefresh (opportunistic, budget-aware) | WorkManager periodic + FCM-triggered |
| Keystore | Secure Enclave (identity keys non-extractable) | StrongBox where available |

## 4. Performance budgets (enforced by startup trace in device-farm CI)

Cold open → interactive chat list ≤ 1.5 s (mid-tier device, NFR-10) · chat open → first messages ≤ 150 ms (local read) · JS bundle ≤ 8 MB (hermes bytecode) · memory steady-state ≤ 300 MB · battery: background ≤ 2%/day (push-driven only, NFR-22).

## 5. Offline UX rules

Everything readable + composable offline; sends queue visibly ("clock" state); banner on degraded connectivity; media downloads resumable; story/status views queue receipts. Airplane-mode test is a release gate.

## 6. OTA & release

Expo Updates for JS-layer fixes (respecting store policies — never native-behavior changes OTA); native releases via store lanes + staged rollout 5→25→100%; crash gate: halt rollout if crash-free < 99.5% (GlitchTip signal).
