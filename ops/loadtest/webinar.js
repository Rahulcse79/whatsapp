// Webinar load profile (T9.x webinar mode, added for T15.01).
//
//   Shape:         one live webinar; N attendees join at once (a "doors open"
//                  thundering herd), then raise hands, ask and upvote questions
//                  while the host polls the roster
//   Pass criteria: join p95 ≤ 1 s · roster read p95 ≤ 500 ms · Q&A write
//                  p95 ≤ 500 ms · no join failures
//
// Why this profile exists: a webinar is the one meeting shape where a large
// audience hits the SAME room record simultaneously. `callsurge.js` measures
// 300 *independent* 1:1 setups; this measures contention on a single webinar's
// participant roster and question list — a row-level hotspot rather than a
// spread of independent writes. That contention is the thing a capacity
// baseline needs to characterise.
//
// Pure HTTP (control plane only), so it runs today without the wsv1 codec shim
// (see README "The wsv1 codec seam"). The media plane is LiveKit's and is not
// exercised here.
//
// Run (against a staging deployment):
//   k6 run -e API_URL=https://staging.wa \
//          -e WEBINAR_ID=<webinar uuid> \
//          -e HOST_TOKEN=<host jwt> \
//          -e TOKENS=<attendee jwt1,jwt2,…> \
//          ops/loadtest/webinar.js

import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";

const API = __ENV.API_URL || "http://localhost:8080";
const WEBINAR_ID = __ENV.WEBINAR_ID || "";
const HOST_TOKEN = __ENV.HOST_TOKEN || "";
const TOKENS = (__ENV.TOKENS || "").split(",").filter(Boolean); // one attendee bearer per VU
const ATTENDEES = Number(__ENV.ATTENDEES || 500); // simultaneous joins ("doors open")
const RAMP = __ENV.RAMP || "30s"; // how fast the audience arrives
const HOLD = __ENV.HOLD || "90s"; // how long the audience stays
const DURATION = __ENV.DURATION || "2m"; // host-poll window; shorten for a smoke run

// ── metrics ──────────────────────────────────────────────────────────────
const joinLatency = new Trend("webinar_join_ms", true);
const rosterLatency = new Trend("webinar_roster_ms", true);
const questionLatency = new Trend("webinar_question_ms", true);
const upvoteLatency = new Trend("webinar_upvote_ms", true);
const handLatency = new Trend("webinar_hand_ms", true);
const joined = new Counter("webinar_joins_ok");
const joinFail = new Counter("webinar_join_fail");
const rosterFail = new Counter("webinar_roster_fail");

export const options = {
  scenarios: {
    // The audience arrives over RAMP and then stays for the session.
    audience: {
      executor: "ramping-vus",
      exec: "attend",
      startVUs: 0,
      stages: [
        { duration: RAMP, target: ATTENDEES },
        { duration: HOLD, target: ATTENDEES },
      ],
      gracefulRampDown: "10s",
    },
    // The host polls the roster throughout — the read side of the same hot row.
    host: {
      executor: "constant-vus",
      exec: "hostPoll",
      vus: 1,
      duration: DURATION,
    },
  },
  thresholds: {
    webinar_join_ms: ["p(95)<=1000"],
    webinar_roster_ms: ["p(95)<=500"],
    webinar_question_ms: ["p(95)<=500"],
    webinar_join_fail: ["count==0"], // everyone who knocks must get in (or be queued)
    webinar_roster_fail: ["count==0"],
    checks: ["rate==1.0"],
  },
};

function authHeaders(token) {
  return { headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" } };
}

/** attend is one audience member: join, raise a hand, ask a question, upvote. */
export function attend() {
  const token = TOKENS[(__VU - 1) % Math.max(TOKENS.length, 1)];
  if (!WEBINAR_ID || !token) {
    joinFail.add(1);
    return;
  }
  const hdr = authHeaders(token);

  // 1. Join — the thundering-herd write against one webinar row.
  const jStart = Date.now();
  const jres = http.post(`${API}/v1/webinars/${WEBINAR_ID}/join`, null, hdr);
  joinLatency.add(Date.now() - jStart);
  const jok = check(jres, {
    "join 200": (r) => r.status === 200,
    "join returns status+role": (r) => {
      try {
        const b = r.json();
        return Boolean(b && b.status && b.role);
      } catch {
        return false;
      }
    },
  });
  if (jok) joined.add(1);
  else {
    joinFail.add(1);
    return;
  }

  // 2. A slice of the audience raises a hand.
  if (__VU % 5 === 0) {
    const hStart = Date.now();
    const hres = http.post(`${API}/v1/webinars/${WEBINAR_ID}/hand`, JSON.stringify({ raised: true }), hdr);
    handLatency.add(Date.now() - hStart);
    check(hres, { "hand accepted (204)": (r) => r.status === 204 });
  }

  // 3. A slice asks a question; the rest upvote what is already there — the
  //    realistic Q&A ratio, and the upvote path is the contended one.
  if (__VU % 10 === 0) {
    const qStart = Date.now();
    const qres = http.post(
      `${API}/v1/webinars/${WEBINAR_ID}/questions`,
      JSON.stringify({ body: `load question from vu ${__VU}` }),
      hdr,
    );
    questionLatency.add(Date.now() - qStart);
    check(qres, { "question created (201)": (r) => r.status === 201 });
  } else {
    const qs = http.get(`${API}/v1/webinars/${WEBINAR_ID}/questions`, hdr);
    if (qs.status === 200) {
      let list = [];
      try {
        list = qs.json().questions ?? [];
      } catch {
        list = [];
      }
      const target = list[__VU % Math.max(list.length, 1)];
      if (target && target.id) {
        const uStart = Date.now();
        const ures = http.post(`${API}/v1/webinars/${WEBINAR_ID}/questions/${target.id}/upvote`, null, hdr);
        upvoteLatency.add(Date.now() - uStart);
        check(ures, { "upvote accepted (204)": (r) => r.status === 204 });
      }
    }
  }

  sleep(3); // an attendee is mostly idle — they are watching, not hammering
}

/** hostPoll is the presenter's client refreshing the roster. */
export function hostPoll() {
  if (!WEBINAR_ID || !HOST_TOKEN) {
    rosterFail.add(1);
    return;
  }
  const started = Date.now();
  const res = http.get(`${API}/v1/webinars/${WEBINAR_ID}/roster`, authHeaders(HOST_TOKEN));
  rosterLatency.add(Date.now() - started);
  const ok = check(res, { "roster 200": (r) => r.status === 200 });
  if (!ok) rosterFail.add(1);
  sleep(2); // the host UI refreshes every couple of seconds
}

export function handleSummary(data) {
  const m = data.metrics;
  const num = (name, stat) => Math.round(m[name]?.values[stat] ?? 0);
  const cnt = (name) => m[name]?.values.count ?? 0;
  return {
    stdout:
      `\nwebinar:\n` +
      `  joins ok=${cnt("webinar_joins_ok")} fail=${cnt("webinar_join_fail")} (fail must be 0)\n` +
      `  join p95=${num("webinar_join_ms", "p(95)")}ms (target ≤ 1000)\n` +
      `  roster p95=${num("webinar_roster_ms", "p(95)")}ms (target ≤ 500)\n` +
      `  question p95=${num("webinar_question_ms", "p(95)")}ms (target ≤ 500)\n` +
      `  upvote p95=${num("webinar_upvote_ms", "p(95)")}ms · hand p95=${num("webinar_hand_ms", "p(95)")}ms\n`,
  };
}
