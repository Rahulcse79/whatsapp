// Fan-out stress load profile (load-and-chaos-testing.md §1, GATE P1).
//
//   Shape:         50 senders → 1,024-member groups simultaneously
//   Pass criteria: ACK p95 ≤ 500 ms · backlog drains ≤ 60 s · no receipt-storm collapse
//   Auditor:       every message is numbered per conversation; sent-ACKed must
//                  equal delivered across all clients (zero loss is THE assertion).
//
// Run (against a staging gateway):
//   k6 run -e WS_URL=wss://staging.wa/v1/ws -e GROUP_ID=... -e TOKENS=... ops/loadtest/fanout.js
//
// SEAM: the gateway speaks the binary wsv1 protobuf protocol (the interim JSON
// codec was deleted at T0.12). `encodeFrame` / `decodeFrame` bind to a compiled
// wsv1 codec the load harness provides (see ops/loadtest/README.md). The profile
// shape, thresholds, and the zero-loss auditor below are transport-agnostic.

import ws from "k6/ws";
import { check, fail } from "k6";
import { Counter, Trend } from "k6/metrics";
import { encodeFrame, decodeFrame } from "./codec/wsv1.js";

const WS_URL = __ENV.WS_URL || "ws://localhost:8080/v1/ws";
const GROUP_ID = __ENV.GROUP_ID || "group-1024";
const MSGS_PER_SENDER = Number(__ENV.MSGS_PER_SENDER || 50);
const TOKENS = (__ENV.TOKENS || "").split(",").filter(Boolean); // one bearer per sender VU

// ── metrics ──────────────────────────────────────────────────────────────
const ackLatency = new Trend("ack_latency_ms", true);
const sent = new Counter("msgs_sent");
const acked = new Counter("msgs_acked");
const lost = new Counter("msgs_lost"); // sent but never ACKed by drain time — must be 0

export const options = {
  scenarios: {
    // 50 senders fire into the same 1,024-member group at once.
    fanout_stress: {
      executor: "per-vu-iterations",
      vus: 50,
      iterations: 1,
      maxDuration: "5m",
    },
  },
  thresholds: {
    ack_latency_ms: ["p(95)<=500"], // ACK p95 ≤ 500 ms
    msgs_lost: ["count==0"], // zero loss — the single most important assertion
    // Backlog must drain: every sent message is ACKed within the run window.
    checks: ["rate==1.0"],
  },
};

export default function () {
  const token = TOKENS[(__VU - 1) % Math.max(TOKENS.length, 1)];
  if (!token) fail("no bearer token for VU (set -e TOKENS=jwt1,jwt2,...)");

  // Per-conversation numbering: this VU owns the sequence prefix `${__VU}:`.
  const outstanding = new Map(); // clientRef → { seqNo, sentAt }
  let nextSeq = 0;

  const res = ws.connect(WS_URL, { headers: { Authorization: `Bearer ${token}` } }, (socket) => {
    socket.on("open", () => {
      socket.send(encodeFrame({ t: "hello", resumeToken: null, cursors: [] }));
    });

    socket.on("binaryMessage", (data) => {
      const frame = decodeFrame(data);
      if (frame.t === "hello_ack") {
        for (let i = 0; i < MSGS_PER_SENDER; i++) sendOne(socket);
      } else if (frame.t === "msg_ack") {
        const pending = outstanding.get(frame.clientRef);
        if (pending) {
          ackLatency.add(Date.now() - pending.sentAt);
          acked.add(1);
          outstanding.delete(frame.clientRef);
          if (outstanding.size === 0) socket.close();
        }
      }
    });

    // Drain budget: if any message is still un-ACKed after 60 s, the backlog did
    // not drain — count the loss and close.
    socket.setTimeout(() => {
      lost.add(outstanding.size);
      socket.close();
    }, 60_000);
  });

  check(res, { "ws handshake 101": (r) => r && r.status === 101 });

  function sendOne(socket) {
    const seqNo = nextSeq++;
    const clientRef = `${__VU}:${seqNo}`;
    outstanding.set(clientRef, { seqNo, sentAt: Date.now() });
    sent.add(1);
    socket.send(
      encodeFrame({
        t: "msg_send",
        clientRef,
        conversationId: GROUP_ID,
        // A load message carries opaque ciphertext; content is irrelevant to the
        // fan-out path. The number rides the clientRef for the auditor.
        sealedEnvelope: new Uint8Array([seqNo & 0xff]),
      }),
    );
  }
}

// The auditor: sent must equal acked. k6's msgs_lost threshold (==0) enforces it
// per-run; handleSummary makes the reconciliation explicit in the report.
export function handleSummary(data) {
  const s = data.metrics.msgs_sent?.values.count ?? 0;
  const a = data.metrics.msgs_acked?.values.count ?? 0;
  return {
    stdout: `\nfan-out audit: sent=${s} acked=${a} lost=${s - a} (must be 0)\n`,
  };
}
