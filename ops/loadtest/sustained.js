// Sustained profile (load-and-chaos-testing.md §1): 20k concurrent WS, ~300 msg/s
// ingress, a realistic mix (12% media, 6% in calls), held 24 h. Pass = all SLOs
// green AND zero loss (the §4 durability audit). This is the profile P4 exit
// requires green for two consecutive weeks with chaos enabled.
//
// SEAM: the gateway speaks binary wsv1 protobuf; encodeFrame/decodeFrame bind to
// the compiled codec the harness supplies (ops/loadtest/README.md).

import ws from "k6/ws";
import { check } from "k6";
import { auditSummary, auditThresholds, newLedger } from "./auditor.js";
import { decodeFrame, encodeFrame } from "./codec/wsv1.js";

const WS_URL = __ENV.WS_URL || "ws://localhost:8080/v1/ws";
const TOKENS = (__ENV.TOKENS || "").split(",").filter(Boolean);
const CONVS = (__ENV.CONVS || "").split(",").filter(Boolean);
const HOLD = __ENV.HOLD || "24h";
const SEND_EVERY_MS = Number(__ENV.SEND_EVERY_MS || 60000); // 20k VUs / 60s ≈ 330 msg/s

export const options = {
  scenarios: {
    sustained: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "5m", target: 20000 }, // ramp in
        { duration: HOLD, target: 20000 }, // hold
        { duration: "2m", target: 0 }, // drain
      ],
      gracefulStop: "2m",
    },
  },
  thresholds: {
    ...auditThresholds,
    ack_latency_ms: ["p(95)<=250"], // send → ACK SLO
    deliver_latency_ms: ["p(95)<=1000"], // ACK → delivered SLO
  },
};

// The realistic mix: most ticks are text; a fraction are media / call setups.
// They all ride the SAME numbered ledger, so the durability audit covers them.
function kindForTick(n) {
  const r = n % 100;
  if (r < 6) return "call"; // 6% in calls
  if (r < 18) return "media"; // 12% media
  return "text";
}

export default function () {
  const token = TOKENS[(__VU - 1) % Math.max(TOKENS.length, 1)];
  const conv = CONVS[(__VU - 1) % Math.max(CONVS.length, 1)];
  if (!token || !conv) return;
  const ledger = newLedger(__VU);
  let ticks = 0;

  const res = ws.connect(WS_URL, { headers: { Authorization: `Bearer ${token}` } }, (socket) => {
    socket.on("open", () => socket.send(encodeFrame({ t: "hello", resumeToken: null, cursors: [] })));
    socket.on("binaryMessage", (data) => {
      const f = decodeFrame(data);
      if (f.t === "msg_ack") ledger.onAck(f.clientRef);
      else if (f.t === "inbox_batch") for (const it of f.items) ledger.onDeliver(it.msgUuid);
    });

    socket.setInterval(() => {
      const ref = ledger.nextRef();
      const kind = kindForTick(ticks++);
      socket.send(
        encodeFrame({ t: "msg_send", clientRef: ref, conversationId: conv, kind, sealedEnvelope: new Uint8Array([ticks & 0xff]) }),
      );
    }, SEND_EVERY_MS);

    // The ramping-vus stage drains VUs; finalize the ledger on close.
    socket.on("close", () => ledger.finalize());
  });
  check(res, { "ws handshake 101": (r) => r && r.status === 101 });
}

export function handleSummary(data) {
  return { stdout: auditSummary(data) };
}
