// Inbox-soak profile (load-and-chaos-testing.md §1): 60% of recipients stay
// offline for a long window while senders keep sending (the messages accumulate
// in the PG message_inbox), then the offline cohort reconnects en masse and must
// replay its entire backlog exactly once. Pass = replay correctness at scale (the
// §4 audit: every accumulated message delivered, zero loss, no dupes) and healthy
// PG partition behaviour.
//
// Two VU groups: senders send continuously; the offline cohort sleeps through the
// soak, then connects and drains its backlog. The audit reconciles the two.

import ws from "k6/ws";
import { check, sleep } from "k6";
import { Counter } from "k6/metrics";
import { auditSummary, auditThresholds, newLedger } from "./auditor.js";
import { decodeFrame, encodeFrame } from "./codec/wsv1.js";

const WS_URL = __ENV.WS_URL || "ws://localhost:8080/v1/ws";
const SENDER_TOKENS = (__ENV.SENDER_TOKENS || "").split(",").filter(Boolean);
const OFFLINE_TOKENS = (__ENV.OFFLINE_TOKENS || "").split(",").filter(Boolean);
const CONVS = (__ENV.CONVS || "").split(",").filter(Boolean);
const SOAK = __ENV.SOAK || "24h";
const dupes = new Counter("replay_dupes"); // a msg replayed twice — must be 0

export const options = {
  scenarios: {
    // Senders: send throughout the soak into conversations the offline cohort is in.
    senders: {
      executor: "constant-vus",
      exec: "sender",
      vus: 200,
      duration: SOAK,
    },
    // Offline cohort (60%): idle through the soak, then reconnect together and drain.
    offline: {
      executor: "per-vu-iterations",
      exec: "offlineRecipient",
      vus: 300,
      iterations: 1,
      maxDuration: "26h",
      startTime: "0s",
    },
  },
  thresholds: {
    ...auditThresholds,
    replay_dupes: ["count==0"], // exactly-once replay
  },
};

export function sender() {
  const token = SENDER_TOKENS[(__VU - 1) % Math.max(SENDER_TOKENS.length, 1)];
  const conv = CONVS[(__VU - 1) % Math.max(CONVS.length, 1)];
  if (!token || !conv) return;
  const ledger = newLedger(__VU);
  ws.connect(WS_URL, { headers: { Authorization: `Bearer ${token}` } }, (socket) => {
    socket.on("open", () => socket.send(encodeFrame({ t: "hello", resumeToken: null, cursors: [] })));
    socket.on("binaryMessage", (data) => {
      const f = decodeFrame(data);
      if (f.t === "msg_ack") ledger.onAck(f.clientRef);
    });
    socket.setInterval(() => {
      const ref = ledger.nextRef();
      socket.send(encodeFrame({ t: "msg_send", clientRef: ref, conversationId: conv, sealedEnvelope: new Uint8Array([1]) }));
    }, 2000);
    socket.setTimeout(() => socket.close(), 60000);
    socket.on("close", () => ledger.finalize());
  });
}

export function offlineRecipient() {
  const token = OFFLINE_TOKENS[(__VU - 1) % Math.max(OFFLINE_TOKENS.length, 1)];
  if (!token) return;
  sleep(parseDurationS(SOAK)); // stay offline for the soak window

  const seen = new Set();
  const ledger = newLedger(100000 + __VU);
  const res = ws.connect(WS_URL, { headers: { Authorization: `Bearer ${token}` } }, (socket) => {
    // Cursors empty → the gateway replays the full backlog since last ACK.
    socket.on("open", () => socket.send(encodeFrame({ t: "hello", resumeToken: null, cursors: [] })));
    socket.on("binaryMessage", (data) => {
      const f = decodeFrame(data);
      if (f.t !== "inbox_batch") return;
      for (const it of f.items) {
        if (seen.has(it.msgUuid)) dupes.add(1); // a replayed message must appear once
        seen.add(it.msgUuid);
        ledger.onDeliver(it.msgUuid);
        socket.send(encodeFrame({ t: "client_ack", conversationId: it.conversationId, seq: it.seq }));
      }
    });
    socket.setTimeout(() => socket.close(), 120000); // 2 min to drain the backlog
    socket.on("close", () => ledger.finalize());
  });
  check(res, { "reconnect 101": (r) => r && r.status === 101 });
}

function parseDurationS(s) {
  const m = /^(\d+)([smh])$/.exec(s);
  if (!m) return 60;
  const n = Number(m[1]);
  return m[2] === "h" ? n * 3600 : m[2] === "m" ? n * 60 : n;
}

export function handleSummary(data) {
  return { stdout: auditSummary(data) };
}
