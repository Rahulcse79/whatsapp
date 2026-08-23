// Channel broadcast load profile (T7.01/T7.02, added for T15.01).
//
//   Shape:         1 publisher posting into a large-follower channel while N
//                  follower VUs poll the feed and react
//   Pass criteria: publish p95 ≤ 500 ms · feed read p95 ≤ 300 ms · no publish
//                  failures · every published post becomes visible to followers
//                  within the visibility budget
//
// Why this profile exists: a channel post is the one fan-out path that is NOT
// E2EE — the body is server-visible (T7.01), so delivery is a server-side
// broadcast to every follower rather than a per-device sealed envelope. That
// makes it a different load shape from `fanout.js` (1,024-member group,
// per-device sender-key fan-out) and it stresses a different seam: the channel
// post write + the NATS nudge + the follower feed read.
//
// Unlike the WS profiles this one is pure HTTP, so it runs today without the
// wsv1 codec shim (see README "The wsv1 codec seam").
//
// Run (against a staging deployment):
//   k6 run -e API_URL=https://staging.wa \
//          -e CHANNEL_ID=<channel uuid> \
//          -e PUBLISHER_TOKEN=<owner/admin jwt> \
//          -e TOKENS=<follower jwt1,jwt2,…> \
//          ops/loadtest/channel.js

import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";

const API = __ENV.API_URL || "http://localhost:8080";
const CHANNEL_ID = __ENV.CHANNEL_ID || "";
const PUBLISHER_TOKEN = __ENV.PUBLISHER_TOKEN || "";
const TOKENS = (__ENV.TOKENS || "").split(",").filter(Boolean); // one follower bearer per reader VU
const POSTS = Number(__ENV.POSTS || 30); // posts the publisher fires
const READERS = Number(__ENV.READERS || 200); // concurrent follower VUs
const VISIBILITY_BUDGET_MS = Number(__ENV.VISIBILITY_BUDGET_MS || 5000);
const DURATION = __ENV.DURATION || "2m"; // follower window; shorten for a smoke run

// ── metrics ──────────────────────────────────────────────────────────────
const publishLatency = new Trend("channel_publish_ms", true);
const feedLatency = new Trend("channel_feed_read_ms", true);
const reactLatency = new Trend("channel_react_ms", true);
const visibility = new Trend("channel_visibility_ms", true); // publish → visible in a follower feed
const published = new Counter("channel_posts_published");
const publishFail = new Counter("channel_publish_fail");
const feedFail = new Counter("channel_feed_fail");

export const options = {
  scenarios: {
    // The publisher fires a steady stream of posts for the whole run.
    publisher: {
      executor: "shared-iterations",
      exec: "publish",
      vus: 1,
      iterations: POSTS,
      maxDuration: "5m",
      startTime: "0s",
    },
    // Followers poll the feed and react, the read-amplification side of a
    // broadcast: one write, many reads.
    followers: {
      executor: "constant-vus",
      exec: "follow",
      vus: READERS,
      duration: DURATION,
      startTime: "5s", // let the publisher get ahead
    },
  },
  thresholds: {
    channel_publish_ms: ["p(95)<=500"],
    channel_feed_read_ms: ["p(95)<=300"],
    channel_publish_fail: ["count==0"],
    channel_feed_fail: ["count==0"],
    checks: ["rate==1.0"],
  },
};

// Post ids this VU has already counted for the visibility metric (per-VU, since
// each k6 VU is its own JS runtime).
const seen = new Set();

function authHeaders(token) {
  return { headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" } };
}

/** publish posts into the channel and records write latency. */
export function publish() {
  if (!CHANNEL_ID || !PUBLISHER_TOKEN) {
    publishFail.add(1);
    return;
  }
  const body = JSON.stringify({ body: `load post ${__ITER} @ ${Date.now()}`, publish_at_ms: 0 });
  const started = Date.now();
  const res = http.post(`${API}/v1/channels/${CHANNEL_ID}/posts`, body, authHeaders(PUBLISHER_TOKEN));
  publishLatency.add(Date.now() - started);

  const ok = check(res, {
    "post created (201)": (r) => r.status === 201,
    "post has id": (r) => {
      try {
        return Boolean(r.json() && r.json().id);
      } catch {
        return false;
      }
    },
  });
  if (ok) published.add(1);
  else publishFail.add(1);
  sleep(1); // ~1 post/s — a realistic publisher cadence, not a write flood
}

/** follow reads the channel feed the way a follower's client does, then reacts
 *  to the newest post. */
export function follow() {
  const token = TOKENS[(__VU - 1) % Math.max(TOKENS.length, 1)];
  if (!CHANNEL_ID || !token) {
    feedFail.add(1);
    return;
  }

  const started = Date.now();
  const res = http.get(`${API}/v1/channels/${CHANNEL_ID}/posts?limit=20`, authHeaders(token));
  feedLatency.add(Date.now() - started);

  const ok = check(res, { "feed 200": (r) => r.status === 200 });
  if (!ok) {
    feedFail.add(1);
    sleep(1);
    return;
  }

  let posts = [];
  try {
    posts = res.json().posts ?? [];
  } catch {
    feedFail.add(1);
    sleep(1);
    return;
  }

  const newest = posts[0];
  // Visibility is measured on FIRST SIGHTING only. Sampling the newest post's
  // age on every poll would instead measure "how long since the publisher last
  // posted", which climbs forever once publishing stops — a meaningless number.
  if (newest && newest.created_at_ms && !seen.has(newest.id)) {
    seen.add(newest.id);
    // Compared against VISIBILITY_BUDGET_MS in the summary rather than as a hard
    // k6 threshold, because a follower polling on a 1 s cadence already carries
    // up to a poll interval of skew.
    visibility.add(Math.max(0, Date.now() - newest.created_at_ms));

    const rStart = Date.now();
    const rr = http.post(
      `${API}/v1/channel-posts/${newest.id}/react`,
      JSON.stringify({ emoji: "👍" }),
      authHeaders(token),
    );
    reactLatency.add(Date.now() - rStart);
    check(rr, { "react accepted (204)": (r) => r.status === 204 });
  }
  sleep(1); // 1 Hz poll per follower
}

export function handleSummary(data) {
  const m = data.metrics;
  const num = (name, stat) => Math.round(m[name]?.values[stat] ?? 0);
  const cnt = (name) => m[name]?.values.count ?? 0;
  const visP95 = num("channel_visibility_ms", "p(95)");
  return {
    stdout:
      `\nchannel broadcast:\n` +
      `  published ok=${cnt("channel_posts_published")} fail=${cnt("channel_publish_fail")}\n` +
      `  publish p95=${num("channel_publish_ms", "p(95)")}ms (target ≤ 500)\n` +
      `  feed read p95=${num("channel_feed_read_ms", "p(95)")}ms (target ≤ 300)\n` +
      `  react p95=${num("channel_react_ms", "p(95)")}ms\n` +
      `  visibility p95=${visP95}ms (budget ${VISIBILITY_BUDGET_MS}) ${visP95 <= VISIBILITY_BUDGET_MS ? "OK" : "OVER"}\n` +
      `  feed failures=${cnt("channel_feed_fail")} (must be 0)\n`,
  };
}
