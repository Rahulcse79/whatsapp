# Personas, User Stories & Acceptance Criteria

| Doc | Personas + story catalog conventions + exemplar stories |
|---|---|
| Status | v1.0 |
| Note | The **full** story catalog lives per epic in [12-planning/epics.md](../12-planning/epics.md). This doc defines personas, the story format, and fully-worked exemplar stories with acceptance criteria that set the quality bar. |

## 1. Personas

| Persona | Profile | Cares about |
|---|---|---|
| **Priya, 28 — everyday user** | Mid-tier Android, patchy 4G, chats with family, sends lots of voice notes & photos | Speed on bad network, storage frugality, "it just works" |
| **Marco, 41 — community organizer** | Runs a 900-member neighborhood group + PTT channel for volunteers | Group admin tools, announcements mode, PTT reliability |
| **Dr. Chen, 52 — privacy-critical professional** | iPhone, discusses sensitive matters | Verifiable E2EE, safety numbers, disappearing-by-default server storage |
| **Sam, 33 — self-hosting operator** | Runs the platform for a 2,000-person organization on own hardware, sometimes air-gapped | One-box deploy, backups, admin console, no cloud dependencies |
| **Ana, 24 — multi-device power user** | Phone + web PWA + tablet | Seamless device linking, history bootstrap, consistent read states |

## 2. Story format (binding convention)

```
US-<EPIC>-<nn>  As a <persona>, I want <capability> so that <outcome>.
  AC1..ACn  Given/When/Then — every AC is automatable.
  NFR refs  Latency/security constraints that apply.
  E2EE note Required whenever content handling is involved.
```

Definition of Ready: story has ACs + NFR refs + API/doc links. Definition of Done: implemented, unit+integration tested, AC-mapped E2E test green, docs updated, dashboards show the new SLI if any.

## 3. Exemplar stories (quality bar)

### US-MSG-01 — Send a text message (the core loop)

*As Priya, I want my message to reach my sister instantly and reliably, even if one of us is offline, so that I trust the app with important conversations.*

- **AC1** Given both online, when I send, then the recipient renders the message in ≤ 500 ms (p95) and I see ticks progress sending → sent → delivered.
- **AC2** Given recipient offline, when I send, then I see "sent"; the ciphertext is stored server-side; on her reconnect within 30 days she receives it exactly once and I receive "delivered".
- **AC3** Given my radio drops mid-send, when connectivity returns, then the outbox retries with the same UUIDv7 and the server dedupes — the recipient never sees a duplicate.
- **AC4** Given the server is fully compromised, then the attacker obtains only ciphertext + envelope metadata (verified by protocol test capturing server-side bytes).
- **NFR refs:** NFR-05, NFR-12, NFR-15, NFR-16.

### US-GRP-03 — Announcements mode

*As Marco, I want to restrict posting to admins during emergencies so that critical instructions aren't buried.*

- **AC1** Given announcements mode on, when a member sends, then the server rejects with `GROUP_POSTING_RESTRICTED` and the client explains why.
- **AC2** Mode changes propagate to all online members ≤ 2 s as a group event; offline members receive it in sync order before any subsequent message.
- **AC3** Only owner/admins can toggle; audit trail in group metadata.

### US-PTT-01 — Grab the floor

*As Marco's volunteer, I want to press-and-hold and be heard immediately so that coordination feels like a radio.*

- **AC1** Given nobody holds the floor, press → my audio reaches listeners in ≤ 200 ms (p95).
- **AC2** Given the floor is held, press → I get queue position feedback ≤ 150 ms; grant arrives FIFO on release.
- **AC3** Given my network dies while speaking, then the floor auto-releases ≤ 1 s (missed heartbeats) and passes to next in queue; my stale audio cannot resume (fencing seq — verified by protocol test).

### US-SYNC-02 — Link the web client

*As Ana, I want to scan a QR on web and get my chats there so that I can type on a keyboard.*

- **AC1** Scan → linked and chat list usable ≤ 30 s on 10 Mbps; history transfer is E2E-encrypted from the primary device; server relays only ciphertext.
- **AC2** The new device appears in my device list on all devices; a signed device-list update prevents silent injection (safety-number check unchanged for contacts unless identity key changes).
- **AC3** Revoking the web device from my phone kills its session ≤ 5 s and invalidates its prekeys and push route.

### US-ADMIN-02 — Action an abuse report

*As Sam, I want to review a report and suspend a spammer so that my community stays usable — without ever being able to read anyone's chats.*

- **AC1** Report queue shows reporter-consented ciphertext-forwarded content only; absent consent, only metadata (fan-out rate, account age).
- **AC2** Suspend takes effect ≤ 5 s (sessions killed, sends rejected with `ACCOUNT_SUSPENDED`).
- **AC3** The action writes an immutable `audit_log` row; owner role can review all admin actions.

## 4. Story inventory pointer

Every epic in [epics.md](../12-planning/epics.md) lists its stories as one-liners; any story entering a sprint must be expanded to this format first, in that file.
