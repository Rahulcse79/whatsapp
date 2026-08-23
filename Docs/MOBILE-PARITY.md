# Mobile parity (T5.01 → Phase 16) — audit and plan

Status: planning complete, execution starting. Scope: `clients/mobile` (Expo /
React Native), bringing it to feature parity with `clients/web`.

## 1. The gap, measured

| | web | mobile |
|---|---|---|
| Screens | ~21 | 5 (login, verify, chats, thread, search) |
| `appServices` methods | 207 | 10 |
| `appServices` lines | 2,576 | ~300 |
| Feature groups | 28 | 2 (auth, basic send) |
| Design system | tokens + `Screen`/`Section`/`EmptyState` (U1–U5) | none |

Mobile *does* already have, and these do not need rebuilding:

- the **binary protobuf WS transport** (`src/platform/wsTransport.ts`) — the
  runner's "bring the protobuf transport to mobile" note is stale, it landed;
- platform adapters for SQLite, secure store, HTTP, scheduler, link preview;
- the call stack (`src/call/`: CallKeep ringer, VoIP push, LiveKit media,
  camera, screen share) — more platform-specific work than the web has;
- media plumbing (`src/ui/media/`).

## 2. The decision that shapes everything: extract, don't duplicate

The naive reading of "parity" is *port 197 service methods from web to mobile*.
That is the wrong move: it doubles the surface permanently, and every future
task then has to be written twice or silently regresses one client. This session
alone added T13.02, T14.01 and T15.05 to web only — the gap is *widening* at the
rate features land.

So Phase 16 starts by **moving the service layer into a shared package**, with
the two clients keeping only their platform adapters and their UI.

Feasibility was measured, not assumed. Platform coupling in the web's 2,576-line
`appServices.ts`:

| Coupling | Count | Resolution |
|---|---|---|
| `localStorage` | 31 uses, 14 keys — all client-local prefs (`wa.drafts`, `wa.mute.*`, `wa.templates`, …) | a `KeyValueStore` port; web → `localStorage`, mobile → AsyncStorage/SQLite |
| `navigator.*` | 4 (clipboard, vibrate) | a small `DeviceCapabilities` port |
| `AudioContext` | 1 (notification beep) | same port |
| `window.*` | 1 | same port |
| raw `fetch(` | 2 (the rest already funnel through `authedRequest`) | the existing `HttpClient` port |

That is ~38 call sites behind three small ports, to share ~2,400 lines. The
existing `@wa/client-core` already defines `HttpClient`, `SecureStore` and
`Scheduler` ports, so this extends an established pattern rather than inventing
one.

## 3. Phases

Each is a checkpoint: green typecheck + tests, committed, CI verified.

- [ ] **M1 — Shared service layer.** Add `KeyValueStore` + `DeviceCapabilities`
  ports to `@wa/client-core`. Move `appServices.ts` into a new
  `@wa/app-services` package behind those ports. Web switches to it and must
  behave identically — this step ships **no new features and no UI change**, so
  any web regression is a bug in the extraction.
- [ ] **M2 — Mobile adapters + wiring.** Implement the new ports for Expo, wire
  the shared service into the mobile app, and prove the existing 5 screens still
  work against it.
- [ ] **M3 — Mobile design system.** Port the U1 token layer to React Native
  (`StyleSheet` + a theme context), and build the RN equivalents of `Screen`,
  `Section`, `EmptyState`, buttons, fields, switches, list rows.
- [ ] **M4 — Messaging parity.** Contacts, new chat, create group, group info,
  profile, settings. The screens a user needs before the app is usable daily.
- [ ] **M5 — Rich messaging.** Composer tools, media send/receive, polls,
  location, contact cards, interactive messages, reactions, replies, search.
- [ ] **M6 — Calls + status.** Wire the existing call stack to parity UI;
  stories/status.
- [ ] **M7 — Communities surface.** Channels, communities, discover.
- [ ] **M8 — Long tail.** Collab notes/tasks, whiteboard, multi-device,
  notification prefs, payments surface.
- [ ] **M9 — Verification.** Run both clients against one backend and check
  cross-client delivery, then a device/simulator pass.

## 4. Rules

1. **No feature lands web-only from here.** A task that touches `appServices`
   now touches the shared package, so both clients get it.
2. **M1 changes no behaviour.** It is a refactor; the web app must be
   indistinguishable before and after.
3. **Platform differences stay in adapters**, never in feature code.
4. Each phase is independently shippable and independently revertible.

## 5. Honest estimate

M1–M2 are the load-bearing work and the riskiest (a bad extraction breaks the
working web client). M4–M8 are broad but mechanical once M1–M3 exist. This is
several sessions, not one; the phase list is the checkpoint structure for that.
