// Zero-loss durability auditor (load-and-chaos-testing.md §4, NFR-12) — the
// single most important assertion in the load estate. Every load client numbers
// its messages per conversation; the auditor reconciles sent → ACKed → delivered
// and fails the run on ANY gap. A message the server ACKed (durable in the PG
// inbox) that never reaches a recipient is loss; the msgs_lost==0 threshold is
// the proof. Factored here so every profile audits identically.

import { Counter, Trend } from "k6/metrics";

export const sent = new Counter("msgs_sent");
export const acked = new Counter("msgs_acked");
export const delivered = new Counter("msgs_delivered");
export const lost = new Counter("msgs_lost"); // ACKed but never delivered by drain — MUST be 0
export const ackLatency = new Trend("ack_latency_ms", true);
export const deliverLatency = new Trend("deliver_latency_ms", true);

// Spread into a profile's options.thresholds so every profile enforces zero loss.
export const auditThresholds = {
  msgs_lost: ["count==0"],
  checks: ["rate==1.0"],
};

/**
 * newLedger tracks one VU's messages by a per-conversation sequence
 * (`${vu}:${seq}`). Call nextRef() on send, onAck()/onDeliver() as the frames
 * come back, and finalize() at drain: anything the server ACKed but that never
 * delivered is counted as loss.
 */
export function newLedger(vu) {
  const outstanding = new Map(); // clientRef → { sentAt, ackedAt }
  let seq = 0;
  return {
    nextRef() {
      const ref = `${vu}:${seq++}`;
      outstanding.set(ref, { sentAt: Date.now(), ackedAt: 0 });
      sent.add(1);
      return ref;
    },
    onAck(ref) {
      const e = outstanding.get(ref);
      if (!e) return;
      e.ackedAt = Date.now();
      ackLatency.add(e.ackedAt - e.sentAt);
      acked.add(1);
    },
    onDeliver(ref) {
      delivered.add(1); // count every delivery (the audit reconciles cross-client)
      const e = outstanding.get(ref);
      if (e) {
        if (e.ackedAt) deliverLatency.add(Date.now() - e.ackedAt);
        outstanding.delete(ref); // this VU's own message came back (self-sync)
      }
    },
    finalize() {
      for (const e of outstanding.values()) {
        if (e.ackedAt) lost.add(1); // durable (ACKed) yet undelivered → loss
      }
      outstanding.clear();
    },
    outstanding() {
      return outstanding.size;
    },
  };
}

/** auditSummary renders the run-wide reconciliation for handleSummary. */
export function auditSummary(data) {
  const n = (m) => data.metrics[m]?.values.count ?? 0;
  const s = n("msgs_sent");
  const a = n("msgs_acked");
  const d = n("msgs_delivered");
  const l = n("msgs_lost");
  return `\ndurability audit: sent=${s} acked=${a} delivered=${d} lost=${l} (lost must be 0)\n`;
}
