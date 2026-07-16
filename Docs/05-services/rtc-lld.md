# LLD — RTC: call-ctl + LiveKit + coturn

| Doc | Media plane topology + control plane detail |
|---|---|
| Status | v1.0 · LiveKit ×2 (host-network 16 vCPU nodes) + coturn; call-ctl lives in core-api |

## 1. Topology

```
call-ctl (core-api)  ── admin API / webhooks ──  LiveKit pool (room pinned to node)
      │ mints join JWTs (60 s TTL, room-scoped grants)
clients ── WSS signaling ── ws-gateway            coturn (STUN + TURN/UDP/TCP/443)
clients ══ SRTP media (E2EE frames) ══ LiveKit / or P2P-looking via TURN relay
```

- Room → node assignment: LiveKit-internal registry; node loss = rooms on it drop, clients auto-rejoin via `POST /calls/{room}/rejoin` (fresh token) — reconnect < 5 s target.
- coturn: ~10–20% of calls relay; TURN/TCP-443 as hostile-network last resort; credentials = short-lived HMAC (shared secret, 10-min TTL).

## 2. E2EE media (HLD §15.1)

DTLS-SRTP transport + **insertable-streams frame encryption (SFrame-style)**; frame keys derived from participants' Signal sessions and distributed via WS E2EE signaling; SFU forwards packets it cannot read. Key rotation on participant join/leave (call-ctl signals epoch bump). Recording: client-side w/ consent signal only; server egress exists solely for explicit transport-encrypted org rooms with persistent banner (HLD §10.6).

## 3. Simulcast & quality ladder

| Layer | Spec | Use |
|---|---|---|
| f | 720p / ~1.2 Mbps | good downlink, ≤ 4 visible tiles |
| h | 360p / ~400 kbps | grids, moderate links |
| q | 90–180p / ~100 kbps | poor links; below floor → auto audio-only |

Voice: Opus 6–32 kbps adaptive, DTX + in-band FEC (survives ~30% loss). Screen share = separate track, content-optimized (detail preset, low fps). Active-speaker detection drives layout + subscription switching (server-side, audio-level based — works under E2EE since levels ride RTP header extensions).

## 4. PTT enforcement path (control ↔ media handshake)

```
PttGrant{fence} → call-ctl → LiveKit UpdateParticipant(publish=true, metadata: fence)
release/lapse   → UpdateParticipant(publish=false)  [idempotent, fence-tagged]
```
Pre-negotiated muted tracks mean grant = permission flip + client unmute — no SDP renegotiation on the 200 ms path. A stale speaker's RTP is dropped at the SFU (publish=false since its fence was superseded).

## 5. Capacity (per LiveKit node, 16 vCPU/10 GbE)

| Workload | Capacity |
|---|---|
| Audio-only participants | ~3,000 |
| Video participants (simulcast) | ~1,000–1,500 subscribed streams |
| PTT room 500 listeners | ~20 Mbps egress — trivial; 1-publisher SFU is the cheapest workload |
| 20k-tier peak (§3 HLD): 960 audio + 240 video | < 1 Gbps total across 2 nodes ✅ |

## 6. Webhooks (LiveKit → call-ctl)

`room_started/finished`, `participant_joined/left`, `track_published` → reconcile ring machine, persist call_records, detect zombie rooms (finished but ring active → force-end), feed analytics counters. Webhook auth: shared-secret signature; idempotent handlers (webhook redelivery happens).
