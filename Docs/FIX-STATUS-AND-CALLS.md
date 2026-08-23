# Status + Calling: analysis, root causes, and fix plan

Scope: the Status/stories feature and the voice/video calling stack, server and
clients. Written from a read of `internal/stories`, `internal/calls`,
`@wa/call-engine`, and both clients' call/status code.

---

## Part 1 — Calling

### Symptom
Ringing works; the call connects; **no audio and no video ever arrive**. Affects
both voice and video.

### Root cause (C1): the two peers derive different E2EE keys, so every media frame is dropped

Media is end-to-end encrypted with SFrame over insertable streams
(`clients/web/src/call/frameTransform.ts`). `CallCrypto` derives two keys per
call (`call-engine/src/callCrypto.ts`):

```ts
sendKey = deriveFrameKey(rootSecret, { roomId, epoch, senderId: selfId })
recvKey = deriveFrameKey(rootSecret, { roomId, epoch, senderId: peerId })
```

For two peers to interoperate, **both the root secret and the per-sender ids
must agree**: caller's `selfId` must equal callee's `peerId`, and the root
secret must be identical.

Neither holds today, because the two sides use **different identity spaces**:

| | `selfId` | `peerId` |
|---|---|---|
| Caller | `sessions.current().deviceId` — a **device** id | `peerOf(conversationId)` — a **user** id |
| Callee | `sessions.current().deviceId` — a **device** id | `callerUserId` from the offer — a **user** id |

`CallContext.tsx` sets `const selfId = services.sessions.current()?.deviceId`,
while `peerByConv` is populated from `senderUserId` / `userId` — user ids.

So the root secret, which is
`sha256("wa-call-dev|seed|" + sorted(selfId, peerId) + "|" + roomId)`, is
computed over `{callerDeviceId, calleeUserId}` on one side and
`{calleeDeviceId, callerUserId}` on the other. **They never match.** The
per-sender frame key mismatches for the same reason.

The consequence is silent by design: `pipe()` in `frameTransform.ts` catches a
failed `open()` and **drops the frame** rather than forwarding plaintext — which
is the right security choice, and is why the call looks connected but carries
nothing.

This also explains why it fails specifically in Chromium: `installSenderE2EE`
returns `false` where `createEncodedStreams` is absent, so a browser *without*
insertable streams would carry media in the clear and appear to work.

**Fix:** use one identity space — user ids — on both sides. Present in both the
web and mobile `CallContext`.

### Secondary findings

- **C2 — no diagnosis surface.** A dropped frame is indistinguishable from a
  silent peer. There is no counter, no log, nothing to tell an operator that
  decryption is failing rather than the mic being muted.
- **C3 — no permission handling.** `setMicrophoneEnabled(true)` can reject
  (denied/absent mic) and nothing catches it, so the call proceeds mute with no
  error surfaced.
- **C4 — remote video attaches to `document.body`.** `liveKitRtc.ts` appends
  remote tracks to a detached sink div on `body`, styled `.call-remote-video`,
  rather than into the call overlay — fragile and unstyleable from the overlay.

---

## Part 2 — Status

### Symptom
Posting appears to work, but **another user's status never shows its content** —
it renders as an encrypted placeholder.

### Root cause (S1): story content is never transmitted to viewers

The content path is local-only:

- `postStory()` writes the payload to `localStorage` under `wa.story.{id}` via
  `saveStoryContent()` — **on the author's device only**.
- `GET /v1/stories/feed` returns `{story_id, author, expires_at_ms,
  key_available}` and **nothing else** — no kind, no media reference, no text.
- `loadStoryContent()` reads that same local cache, so for any story this device
  did not post it returns `null` and the viewer sees the placeholder.

The design intends content to ride an E2EE "STORY_KEY" control message
(`media-stories-api.md`), but **that distribution was never implemented**. So a
status is only ever visible to its author, on the device that posted it.

Two things are missing, and both are needed:

1. **The viewer cannot locate the content.** `media_ref` is stored server-side
   (migration 000023 made it a text object key) but is never returned by the
   feed, so a viewer has no way to fetch the ciphertext.
2. **The viewer cannot decrypt it.** The per-story key is never distributed.

### Secondary findings

- **S2 — no reactions or replies.** WhatsApp lets a viewer reply to a status;
  neither exists in the schema, API, or UI.
- **S3 — no real-time arrival.** The feed is polled every 15s (`setInterval` in
  `Status`); a new status from a contact takes up to 15s to appear and nothing
  pushes it.
- **S4 — seen state is one-directional.** `POST /view` records a receipt and the
  author can list viewers, but the *viewer's* own client has no notion of
  "already seen", so the ring never distinguishes seen from unseen.
- **S5 — expiry is server-only.** The 24h purge ticker is wired
  (`core-api/main.go`), and the feed filters on `expires_at`, but the client
  keeps its `wa.story.*` payload cache forever — it is never swept.

### What is already correct (do not rebuild)

- Audience snapshot frozen at post time, author always included
  (`domain.Audience`), so privacy overrides work and you can see your own status.
- 24h hard expiry + purge ticker, and the feed's expiry filter.
- View receipts are idempotent (PK on `(story_id, viewer_id)`), and
  `/viewers` is correctly author-only.
- `View` 404s for a story outside the caller's audience rather than confirming it
  exists.

---

## Fix plan

Ordered so the highest-impact, most-verifiable fix lands first.

- [x] **Phase A — Calling: make media flow.** Unify the call identity space on
  user ids in both clients (C1). Add a decrypt-failure counter and a one-line
  warning so this class of bug is visible next time (C2). Handle a rejected
  mic/camera permission and surface it in the call state (C3). Verify on the
  live stack with two browser sessions.
- [ ] **Phase B — Calling: media plumbing.** Attach remote tracks into the call
  overlay instead of `document.body` (C4), and make hangup/teardown release
  every track.
- [~] **Phase C — Status: make content reach viewers.** *(server half done: the feed now returns `kind` + `media_ref` + `created_at`, verified live. Client half — distributing the per-story payload/key to the audience — is next.)* Return `kind` and
  `media_ref` from the feed so a viewer can locate the content, and distribute
  the per-story payload/key to the audience over the existing E2EE message
  channel at post time. Viewers cache what they receive and render it (S1).
- [ ] **Phase D — Status: seen state + real-time.** Track locally-seen story ids
  so the ring shows seen/unseen like WhatsApp (S4), push new stories to the
  audience so the feed updates without waiting for the poll (S3), and sweep the
  local payload cache on expiry (S5).
- [ ] **Phase E — Status: replies.** A viewer can reply to a status; the reply
  lands in the author's chat as a normal E2EE message quoting the story (S2).
- [ ] **Phase F — Verification.** Two live sessions: post a status from A and
  view it as B; place a voice call and a video call in both directions.
