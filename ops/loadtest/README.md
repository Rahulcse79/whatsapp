# Load test profiles (`ops/loadtest/`)

k6 + custom WS-client load profiles (load-and-chaos-testing.md §1). Every profile
numbers each message per conversation and reconciles **sent-ACKed vs delivered**
after the run — a zero-loss auditor that is the single most important assertion
in the whole test estate.

| Profile | File | Shape | Pass criteria |
|---|---|---|---|
| **Fan-out stress** | [`fanout.js`](fanout.js) | 50 senders → 1,024-member groups simultaneously | ACK p95 ≤ 500 ms · backlog drains ≤ 60 s · zero loss (`msgs_lost==0`) |
| **Call surge** | [`callsurge.js`](callsurge.js) | 300 simultaneous call setups | call setup p95 ≤ 3 s (GATE P2) · every setup opens a ring (`call_setup_fail==0`) |

`callsurge.js` is what **GATE P2** ("call setup p95 ≤ 3 s in staging") runs; its
protocol-level correctness counterpart is scenario **P12** (the ring
offer/answer/decline/timeout + answer-elsewhere transition matrix in
`server/internal/calls/scenario_p12_test.go`).

`fanout.js` is what **GATE P1** ("1,024-member group send … pass in CI load
job") runs. Its protocol-level correctness counterpart is scenario **P6**
(client Sender-Key rotation ordering in `clients/packages/crypto-wrapper/src/
senderKey.p6.test.ts`; server 1,024-way fan-out + aggregate receipts in
`server/internal/fanout/scenario_p6_test.go`).

## Running

```bash
k6 run \
  -e WS_URL=wss://staging.example/v1/ws \
  -e GROUP_ID=<a 1,024-member group id> \
  -e TOKENS=<jwt1,jwt2,…> \
  ops/loadtest/fanout.js
```

`TOKENS` is a comma-separated list of bearer JWTs (one per sender VU); the group
must already have 1,024 members so the accept path branches to the async fan-out
worker (`internal/fanout`).

## The wsv1 codec seam

The gateway speaks the **binary wsv1 protobuf** protocol (the interim JSON codec
was removed at T0.12), so the load client must frame protobuf on the wire.
`fanout.js` imports `encodeFrame` / `decodeFrame` from `./codec/wsv1.js`, a thin
codec the load harness supplies by bundling the generated `@wa/proto-types` for
k6 (k6 runs its own JS VM, so the codec is bundled, not `require`d at runtime).
Everything else in the profile — VU shape, the 500 ms ACK p95 threshold, the
60 s drain budget, and the zero-loss auditor — is transport-agnostic and is the
reviewable contract here.
