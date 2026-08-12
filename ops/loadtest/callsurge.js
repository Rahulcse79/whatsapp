// Call-surge load profile (load-and-chaos-testing.md §1, GATE P2).
//
//   Shape:         300 simultaneous call setups
//   Pass criteria: call setup p95 ≤ 3 s (GATE P2), every setup returns a ring
//
// Each VU issues one POST /v1/calls (create room + mint join token + open the
// ring machine + dispatch offer/VoIP push) and measures wall-clock setup latency.
// This is the control-plane surge; the PTT floor-grant surge (≤ 200 ms) lands
// with T3.04.
//
// Run (against a staging deployment):
//   k6 run -e API_URL=https://staging.wa -e TOKENS=jwt1,jwt2,... \
//          -e CALLEES=uid1,uid2,... ops/loadtest/callsurge.js

import http from "k6/http";
import { check } from "k6";
import { Counter, Trend } from "k6/metrics";

const API = __ENV.API_URL || "http://localhost:8080";
const TOKENS = (__ENV.TOKENS || "").split(",").filter(Boolean); // one caller bearer per VU
const CALLEES = (__ENV.CALLEES || "").split(",").filter(Boolean); // callee user ids to ring

const setupLatency = new Trend("call_setup_ms", true);
const setupOk = new Counter("call_setup_ok");
const setupFail = new Counter("call_setup_fail");

export const options = {
  scenarios: {
    call_surge: {
      executor: "per-vu-iterations",
      vus: 300, // 300 simultaneous setups
      iterations: 1,
      maxDuration: "2m",
    },
  },
  thresholds: {
    call_setup_ms: ["p(95)<=3000"], // GATE P2: call setup p95 ≤ 3 s
    call_setup_fail: ["count==0"], // every setup must open a ring
    checks: ["rate==1.0"],
  },
};

export default function () {
  const token = TOKENS[(__VU - 1) % Math.max(TOKENS.length, 1)];
  const callee = CALLEES[(__VU - 1) % Math.max(CALLEES.length, 1)];
  if (!token || !callee) {
    setupFail.add(1);
    return;
  }

  const started = Date.now();
  const res = http.post(`${API}/v1/calls`, JSON.stringify({ callee_ids: [callee], kind: "voice" }), {
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
  });
  setupLatency.add(Date.now() - started);

  const ok = check(res, {
    "call created (201)": (r) => r.status === 201,
    "has ring_id + join_token": (r) => {
      try {
        const b = r.json();
        return Boolean(b && b.ring_id && b.join_token);
      } catch {
        return false;
      }
    },
  });
  if (ok) setupOk.add(1);
  else setupFail.add(1);
}

export function handleSummary(data) {
  const p95 = Math.round(data.metrics.call_setup_ms?.values["p(95)"] ?? 0);
  const ok = data.metrics.call_setup_ok?.values.count ?? 0;
  const fail = data.metrics.call_setup_fail?.values.count ?? 0;
  return { stdout: `\ncall-surge: setups ok=${ok} fail=${fail} · setup p95=${p95}ms (target ≤ 3000)\n` };
}
