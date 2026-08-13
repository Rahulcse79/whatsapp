// Burst profile (load-and-chaos-testing.md §1): ramp to 60k connections in 10 min
// (3× headroom, NFR-01). Pass = connect success ≥ 99.5% during the ramp and
// recovery ≤ 5 min. This stresses the auth + resume stampede path (bottleneck #2:
// jittered backoff, resume tokens skip full auth, edge rate limits).

import ws from "k6/ws";
import { check } from "k6";
import { Counter, Rate } from "k6/metrics";
import { encodeFrame } from "./codec/wsv1.js";

const WS_URL = __ENV.WS_URL || "ws://localhost:8080/v1/ws";
const TOKENS = (__ENV.TOKENS || "").split(",").filter(Boolean);

const connectOk = new Rate("ws_connect_success");
const connects = new Counter("ws_connects");

export const options = {
  scenarios: {
    burst: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "10m", target: 60000 }, // ramp to 3× headroom in 10 min
        { duration: "5m", target: 60000 }, // hold — recovery window
        { duration: "2m", target: 0 },
      ],
      gracefulStop: "1m",
    },
  },
  thresholds: {
    ws_connect_success: ["rate>=0.995"], // ≥ 99.5% handshakes succeed during the ramp
    checks: ["rate>=0.995"],
  },
};

export default function () {
  const token = TOKENS[(__VU - 1) % Math.max(TOKENS.length, 1)];
  if (!token) {
    connectOk.add(false);
    return;
  }
  const res = ws.connect(WS_URL, { headers: { Authorization: `Bearer ${token}` } }, (socket) => {
    socket.on("open", () => {
      connects.add(1);
      socket.send(encodeFrame({ t: "hello", resumeToken: null, cursors: [] }));
      // Hold the connection briefly, then close — the point is the connect storm,
      // not steady traffic.
      socket.setTimeout(() => socket.close(), 30000);
    });
  });
  const ok = check(res, { "ws handshake 101": (r) => r && r.status === 101 });
  connectOk.add(ok);
}
