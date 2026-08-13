// Reconnect-storm profile (load-and-chaos-testing.md §1/§2): hold 20k connections
// sending steadily while an EXTERNAL chaos action SIGKILLs 1/3 of the gateway
// pods. Pass = zero loss (the §4 audit) and full reconnect ≤ 3 min — resume +
// outbox mask the kill (the relay-model invariant: the PG inbox is truth,
// NATS/gateways are transit).
//
// The pod kill is driven outside k6 (chaos-mesh / a kubectl loop; see
// ops/loadtest/README.md). This script generates the load and audits it; the
// clients' jittered backoff + resume-token reconnect is what recovers. k6 re-runs
// default() per VU, which IS the reconnect — per-VU state (ledger + resume token)
// persists across those iterations.

import ws from "k6/ws";
import { check } from "k6";
import { Trend } from "k6/metrics";
import { auditSummary, auditThresholds, newLedger } from "./auditor.js";
import { decodeFrame, encodeFrame } from "./codec/wsv1.js";

const WS_URL = __ENV.WS_URL || "ws://localhost:8080/v1/ws";
const TOKENS = (__ENV.TOKENS || "").split(",").filter(Boolean);
const CONVS = (__ENV.CONVS || "").split(",").filter(Boolean);

const reconnectMs = new Trend("reconnect_ms", true);
const vuState = new Map(); // __VU → { ledger, resumeToken, lastClosedAt }

export const options = {
  scenarios: {
    storm: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "3m", target: 20000 }, // reach 20k
        { duration: "10m", target: 20000 }, // hold while chaos kills 1/3 of gateways
        { duration: "2m", target: 0 },
      ],
      gracefulStop: "3m",
    },
  },
  thresholds: {
    ...auditThresholds, // ZERO LOSS — resume + outbox must mask the kill
    reconnect_ms: ["p(95)<=180000"], // full reconnect ≤ 3 min
  },
};

export default function () {
  const token = TOKENS[(__VU - 1) % Math.max(TOKENS.length, 1)];
  const conv = CONVS[(__VU - 1) % Math.max(CONVS.length, 1)];
  if (!token || !conv) return;

  let st = vuState.get(__VU);
  if (!st) {
    st = { ledger: newLedger(__VU), resumeToken: null, lastClosedAt: 0 };
    vuState.set(__VU, st);
  }

  ws.connect(WS_URL, { headers: { Authorization: `Bearer ${token}` } }, (socket) => {
    socket.on("open", () => {
      if (st.lastClosedAt) reconnectMs.add(Date.now() - st.lastClosedAt); // time to recover
      socket.send(encodeFrame({ t: "hello", resumeToken: st.resumeToken, cursors: [] }));
    });
    socket.on("binaryMessage", (data) => {
      const f = decodeFrame(data);
      if (f.t === "hello_ack") st.resumeToken = f.resumeToken ?? st.resumeToken;
      else if (f.t === "msg_ack") st.ledger.onAck(f.clientRef);
      else if (f.t === "inbox_batch") for (const it of f.items) st.ledger.onDeliver(it.msgUuid);
    });
    socket.setInterval(() => {
      const ref = st.ledger.nextRef();
      socket.send(encodeFrame({ t: "msg_send", clientRef: ref, conversationId: conv, sealedEnvelope: new Uint8Array([1]) }));
    }, 5000);
    // Keep the socket alive ~30 s per iteration; a gateway kill closes it early
    // and k6 re-enters default() (the reconnect).
    socket.setTimeout(() => socket.close(), 30000);
  });

  st.lastClosedAt = Date.now(); // the socket just closed (clean or killed)
  check(true, { "iteration completed": () => true });
}

export function handleSummary(data) {
  // Everything still outstanding at run end across all VUs is loss.
  for (const st of vuState.values()) st.ledger.finalize();
  return { stdout: auditSummary(data) };
}
