# ADR-004: LiveKit SFU — Operate, Don't Build

Status: **Accepted** · Upstream: HLD §10.1

## Context

Group calls ≤ 32 and PTT rooms ≤ 500 need a server media plane. Full-mesh WebRTC collapses past ~4 participants (upload bandwidth × N). E2EE must survive the media server (it forwards, never decrypts).

## Decision

Self-hosted **LiveKit** SFU pool + **coturn** for STUN/TURN. `call-ctl` (core-api) owns signaling/rooms/tokens; LiveKit owns packets.

## Alternatives

- **mediasoup:** excellent SFU *library*, but we'd build signaling, scaling, recording, and SDK glue — months of media engineering for zero product differentiation. Rejected.
- **Janus:** mature, but C plugin architecture + weaker mobile SDK story. Rejected.
- **Jitsi:** meeting-shaped product, XMPP/Prosody drag, hard to embed in a chat UX. Rejected.
- **MCU (mixing):** server must decrypt → kills E2EE. Rejected outright.
- **Raw mesh:** kept implicitly only as theory; SFU handles 1:1 too (uniform path, TURN reuse).

## Consequences

- ✅ Simulcast, insertable-streams E2EE, RN/iOS/Android/JS SDKs, egress — all shipped.
- ⚠️ Media-plane on-call is real; LiveKit Cloud documented as paid escape hatch (HLD §24).
- Revisit trigger: >32-participant rooms or broadcast tier (V3) → evaluate cascading/streaming.
