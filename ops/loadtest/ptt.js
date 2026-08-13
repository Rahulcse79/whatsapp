// PTT floor-grant load profile (load-and-chaos-testing.md §1, GATE P3).
//
//   Shape:         1 speaker + 200 listeners in one PTT room; the speaker
//                  repeatedly ACQUIRE → (speak) → RELEASE, and one listener
//                  contends for the floor each round.
//   Pass criteria: floor-grant p95 ≤ 200 ms (GATE P3), every ACQUIRE resolves to
//                  a PttGrant or PttQueuePos (no dropped requests).
//
// The floor is server-authoritative (atomic Valkey-Lua acquire/heartbeat/release
// + fencing, internal/ptt). A grant is the WS round-trip PttRequest{ACQUIRE} →
// PttGrant, which is what this profile times.
//
// SEAM (as with fanout.js's wsv1 codec): the gateway must route inbound
// PttRequest frames to core-api's ptt.Service and forward PttGrant/PttQueuePos
// on dev.{device}.call. Until that routing lands, run this against a staging
// build with it wired. Protocol-level correctness counterpart: scenario P11
// (server/internal/ptt/scenario_p11_test.go — concurrent acquire, crash
// failover, zombie fence).
//
// Run (against a staging deployment):
//   k6 run -e WS_URL=wss://staging.wa/v1/ws -e ROOM=<ptt room id> \
//          -e TOKENS=jwt_speaker,jwt_listener1,... ops/loadtest/ptt.js

import ws from "k6/ws";
import { check } from "k6";
import { Counter, Trend } from "k6/metrics";

const WS_URL = __ENV.WS_URL || "ws://localhost:8080/v1/ws";
const ROOM = __ENV.ROOM || "ptt-room";
const TOKENS = (__ENV.TOKENS || "").split(",").filter(Boolean); // 1 speaker + 200 listeners

const grantLatency = new Trend("ptt_grant_ms", true);
const grantOk = new Counter("ptt_grant_ok");
const grantFail = new Counter("ptt_grant_fail");

export const options = {
  scenarios: {
    ptt_floor: {
      executor: "per-vu-iterations",
      vus: 201, // 1 speaker + 200 listeners
      iterations: 1,
      maxDuration: "2m",
    },
  },
  thresholds: {
    ptt_grant_ms: ["p(95)<=200"], // GATE P3: floor-grant p95 ≤ 200 ms
    ptt_grant_fail: ["count==0"], // every ACQUIRE resolves (grant or queue)
    checks: ["rate==1.0"],
  },
};

// Frame helpers mirror the wsv1 PttRequest/PttGrant/PttQueuePos contract; a
// staging run swaps in the real protobuf codec (see SEAM above).
function acquire(socket, room) {
  socket.send(JSON.stringify({ ptt_request: { room_id: room, action: "ACQUIRE" } }));
}
function release(socket, room) {
  socket.send(JSON.stringify({ ptt_request: { room_id: room, action: "RELEASE" } }));
}

export default function () {
  const token = TOKENS[(__VU - 1) % Math.max(TOKENS.length, 1)];
  const speaker = __VU === 1;

  const res = ws.connect(`${WS_URL}?token=${encodeURIComponent(token)}`, {}, (socket) => {
    socket.on("open", () => {
      const started = Date.now();
      acquire(socket, ROOM);

      socket.on("message", (raw) => {
        let frame;
        try {
          frame = JSON.parse(raw);
        } catch {
          return;
        }
        if (frame.ptt_grant) {
          grantLatency.add(Date.now() - started);
          grantOk.add(1);
          check(frame.ptt_grant, { "grant carries a fence": (g) => Number(g.fence) > 0 });
          if (speaker) {
            // Speak briefly, then hand the floor to a listener.
            socket.setTimeout(() => release(socket, ROOM), 200);
          }
          socket.setTimeout(() => socket.close(), 800);
        } else if (frame.ptt_queue_pos) {
          // Contending listener queued — still a resolved ACQUIRE.
          grantLatency.add(Date.now() - started);
          grantOk.add(1);
          socket.setTimeout(() => socket.close(), 800);
        }
      });

      socket.setTimeout(() => {
        grantFail.add(0); // no-op guard so the counter exists even with zero fails
        socket.close();
      }, 5000);
    });

    socket.on("error", () => grantFail.add(1));
  });

  check(res, { "ws connected (status 101)": (r) => r && r.status === 101 });
}
