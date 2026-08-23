# T15.01 — Load + capacity re-baseline

**Type:** report only (no production code changed; two load profiles added).
**Question the task asks:** run the k6 suite at target scale and capture where
NATS/Postgres are the bottleneck.

**Answer in one line:** the target-scale run **cannot be executed yet** — not for
want of a cluster alone, but because three concrete prerequisites in this repo
are missing. This report names them, proves each one, and specifies exactly what
to run once they are closed. No latency or throughput numbers are invented here;
where a number is absent it is because nothing has legitimately measured it.

---

## 1. What "target scale" means

From HLD.md and `Docs/…/load-and-chaos-testing.md` §1, the baseline is the
**sustained** profile: 20k concurrent WebSocket connections, ~300 msg/s, 12%
media, 6% calls, held for 24 h, with ACK p95 ≤ 250 ms and deliver p95 ≤ 1 s and
zero loss. GATE P4 additionally requires that green for two consecutive weeks
with the §2 chaos scenarios enabled.

## 2. Suite inventory (after this task)

| Profile | File | Transport | Runnable today |
|---|---|---|---|
| Sustained (GATE P4) | `sustained.js` | WS | ❌ blocked |
| Burst | `burst.js` | WS | ❌ blocked |
| Reconnect storm | `reconnectstorm.js` | WS | ❌ blocked |
| Fan-out (GATE P1) | `fanout.js` | WS | ❌ blocked |
| Inbox soak | `inboxsoak.js` | WS | ❌ blocked |
| Media flood | `mediaflood.js` | HTTP | ✅ |
| Call surge (GATE P2) | `callsurge.js` | HTTP | ✅ |
| PTT floor (GATE P3) | `ptt.js` | WS (no codec) | ✅ |
| **Channel broadcast** | `channel.js` | HTTP | ✅ **new (T15.01)** |
| **Webinar** | `webinar.js` | HTTP | ✅ **new (T15.01)** |

The two new profiles were written HTTP-only deliberately, so they are not
blocked by finding #1 below.

### 2.1 `channel.js` — broadcast fan-out

A channel post is the one fan-out path that is **not** E2EE: the body is
server-visible (T7.01), so delivery is a server-side broadcast to every follower
rather than a per-device sealed envelope. That is a different shape from
`fanout.js` (1,024-member group, per-device sender-key fan-out) and stresses a
different seam — the post write, the NATS nudge, and the follower feed read.
One publisher at ~1 post/s against N polling followers; measures publish,
feed-read, react, and publish→visible latency.
Thresholds: publish p95 ≤ 500 ms, feed read p95 ≤ 300 ms, zero publish failures.

### 2.2 `webinar.js` — single-room thundering herd

A webinar is the one meeting shape where a large audience hits the **same** row
simultaneously. `callsurge.js` measures 300 *independent* 1:1 setups; this
measures contention on one webinar's participant roster and question list — a
row-level hotspot rather than a spread of independent writes, which is exactly
the kind of contention a capacity baseline has to characterise. Audience ramps
to N over `RAMP`, then raises hands, asks and upvotes questions while the host
polls the roster.
Thresholds: join p95 ≤ 1 s, roster p95 ≤ 500 ms, Q&A write p95 ≤ 500 ms, zero
join failures.

---

## 3. Findings — why the baseline cannot run yet

### Finding 1 (blocker): five of eight existing profiles cannot start

`sustained.js`, `burst.js`, `reconnectstorm.js`, `fanout.js` and `inboxsoak.js`
all `import { encodeFrame, decodeFrame } from "./codec/wsv1.js"`. That file does
not exist anywhere in the repository:

```
$ grep -l "codec/wsv1" ops/loadtest/*.js
burst.js  fanout.js  inboxsoak.js  reconnectstorm.js  sustained.js
$ find . -name "wsv1.js" -not -path "*/node_modules/*"
(no results)
```

k6 fails at module resolution, so these profiles do not produce a partial
result — they produce nothing. **This includes the two gate profiles**:
`fanout.js` is GATE P1 and `sustained.js` is GATE P4. The suite's README
documents the codec as something "the load harness supplies", but no harness in
this repo supplies it.

*What closes it:* bundle the generated `@wa/proto-types` wsv1 encoder into a
single k6-compatible ES module at `ops/loadtest/codec/wsv1.js`. k6 runs its own
JS VM with no `require` at runtime, so this must be a build step (esbuild
bundle), not a runtime import.

### Finding 2 (blocker): the app cannot answer "where is the bottleneck"

The task asks to capture whether NATS or Postgres is the constraint. The server
tree contains exactly **seven** OpenTelemetry instruments, and none of them
observe either system:

| Instrument | Where | What it covers |
|---|---|---|
| `http_requests` | `internal/platform/observability/red.go:28` | HTTP RED |
| `http_request_duration` | `red.go:33` | HTTP RED |
| `http_requests_in_flight` | `red.go:40` | HTTP RED |
| `product_signups` | `internal/analytics/metrics.go:22` | product |
| `product_dau` / `product_mau` | `metrics.go` | product |
| `product_crash_free_ratio` | `metrics.go` | product |

A search for JetStream consumer lag, pending counts, or pool statistics returns
nothing:

```
$ grep -rn "NumPending\|ConsumerInfo\|pg_stat\|pool.Stat()" --include="*.go" internal cmd
(no results)
```

So a run today could show *that* p95 degraded but not *where*. Attributing it to
NATS vs Postgres would be guesswork, and guesswork is what T15.02 and T15.03 are
explicitly gated on — introducing Kafka or Cassandra on an unattributed
regression would be the worst possible outcome of this phase.

*What closes it:* instrument the two suspects before the run, not after.
Minimum viable set:
- **NATS/JetStream:** consumer `NumPending` and redelivery count per durable
  consumer (`push.dispatch`, the fan-out worker stream), sampled as an
  observable gauge; plus the existing DLQ depth.
- **Postgres:** `pgxpool.Stat()` (acquired/idle/total conns, acquire duration) as
  observable gauges on every service, and a histogram around the inbox write in
  the fan-out path.
- Scrape both alongside the NATS server's own `/varz`+`/jsz` and Postgres
  `pg_stat_statements` / `pg_stat_activity`, which give the server-side view the
  app cannot.

### Finding 3 (environment): target scale needs the staging cluster

20k concurrent WS connections, 24 h, with chaos — that is a staging-cluster
exercise. The development rig this repo ships (`start.sh`, colima, four local
processes) cannot host it, and a number produced on a laptop would be
misleading rather than merely imprecise. This is the same class of gate as
T4.06 (external pentest) and T4.07 (DR drill), both already tracked as
human/infrastructure gates.

---

## 4. What was actually executed

See §6 for the recorded results. Only the HTTP profiles were exercised, against
the local development rig, at smoke scale — enough to prove the new scripts are
correct against the real API contracts, and **not** enough to say anything about
capacity. Every assertion in the new profiles was verified against the handlers:

| Profile assertion | Verified against |
|---|---|
| `POST /v1/channels/{id}/posts` → 201 + `id` | `internal/channels/http.go`, `PostView` |
| `GET /v1/channels/{id}/posts` → `{"posts":[…]}` | `internal/channels/http.go` |
| `POST /v1/channel-posts/{id}/react` → 204 | `internal/channels/http.go` |
| `POST /v1/webinars/{id}/join` → 200 + `status`,`role` | `internal/webinar/http.go`, `MeResult` |
| `GET /v1/webinars/{id}/roster` → `{"roster":[…]}` | `internal/webinar/http.go` |
| `GET /v1/webinars/{id}/questions` → `{"questions":[…]}` | `internal/webinar/http.go` |
| `POST …/questions` → 201 · `…/upvote` → 204 · `…/hand` → 204 | `internal/webinar/http.go` |

## 5. The run plan, once the blockers are closed

Ordered so each step's result gates the next:

1. **Close Finding 1** (codec bundle) and **Finding 2** (NATS + PG instruments).
2. **Single-profile calibration** on staging, one at a time, recording the
   NATS/PG instruments alongside k6's client-side latencies:
   `callsurge` → `ptt` → `channel` → `webinar` → `mediaflood`.
3. **`fanout.js` at GATE P1 scale** (50 senders × 1,024-member group). This is
   the first profile that should show a queue: watch fan-out consumer
   `NumPending` and inbox write latency together.
4. **`sustained.js` at 20k** for 1 h as a dress rehearsal, then 24 h.
5. **Chaos overlay** (§2 scenarios) only once 24 h is green.
6. **Attribute the knee.** The bottleneck claim must cite a specific instrument
   crossing a specific threshold at a specific offered load — e.g. "at 14k conns
   fan-out consumer NumPending grows monotonically while PG acquire duration
   stays flat ⇒ NATS-side". That sentence, with its graph, is what unlocks
   T15.02/T15.03; without it both stay closed and the answer is "Postgres and
   NATS stay".

## 6. Recorded results

_This section is the only place numbers belong. It records what was measured,
at what scale, on what hardware — so a laptop smoke run can never be mistaken
for a capacity baseline._

### 6.1 Local smoke — new profiles (not a capacity result)

Environment: single developer machine (macOS, colima), all four services on
localhost, PostgreSQL/Valkey/NATS/MinIO in containers on the same host.
Scale: a handful of VUs. **These numbers characterise a laptop, not the system.**

<!-- filled in by the smoke run; see the commit that adds them -->

### 6.2 Staging capacity baseline

Not yet run — blocked on Findings 1–3.

---

## 7. Recommendation

Do **not** open T15.02 (Kafka) or T15.03 (Cassandra). Both are explicitly
evidence-gated, and this task produced no evidence of a bottleneck in either
system — it produced evidence that the instrumentation needed to find one does
not exist yet. The honest next step is a small enabling task (codec bundle +
NATS/PG instruments), after which T15.01 can be re-run for real.
