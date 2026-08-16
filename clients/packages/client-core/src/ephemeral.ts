// Disappearing messages, self-destruct, and view-once (T10.01) — pure, on-device
// logic. Everything here is E2EE: the per-chat timer rides a sealed control
// message, and per-message flags (self-destruct ttl / view-once) ride the sealed
// body, so the server never learns content — only a coarse timer copy for its
// purge backstop. The client is authoritative for when a message vanishes.

export interface DisappearingPreset {
  seconds: number;
  label: string;
}

/** The disappearing-timer choices offered in the chat UI (WhatsApp + secret-chat
 *  short timers). 0 = off. */
export const DISAPPEARING_PRESETS: DisappearingPreset[] = [
  { seconds: 0, label: "Off" },
  { seconds: 30, label: "30 seconds" },
  { seconds: 300, label: "5 minutes" },
  { seconds: 3600, label: "1 hour" },
  { seconds: 86_400, label: "24 hours" },
  { seconds: 604_800, label: "7 days" },
  { seconds: 7_776_000, label: "90 days" },
];

export const MAX_TTL_SECONDS = 7_776_000; // 90 days — matches the server bound

export function isValidTtl(seconds: number): boolean {
  return Number.isFinite(seconds) && seconds >= 0 && seconds <= MAX_TTL_SECONDS;
}

/** expiryAt is `startMs + ttl`, or Infinity when the timer is off. */
export function expiryAt(startMs: number, ttlSeconds: number): number {
  if (!ttlSeconds || ttlSeconds <= 0) return Infinity;
  return startMs + ttlSeconds * 1000;
}

export interface EphemeralMessage {
  id: string;
  /** when the message was sent */
  sentMs: number;
  /** when the recipient read it (undefined = unread) */
  readMs?: number;
  /** per-message self-destruct ttl (seconds); overrides the chat timer, counts from read */
  selfDestructTtl?: number;
  /** view-once media: destroyed the moment it's opened */
  viewOnce?: boolean;
  /** whether a view-once message has been opened */
  opened?: boolean;
}

/** messageExpiry returns when a message disappears, given the chat's timer:
 *  - view-once: gone the instant it's opened (Infinity until then);
 *  - self-destruct ttl: counts from read (unread never self-destructs);
 *  - chat timer: counts from sent.
 *  Precedence: view-once > self-destruct > chat timer. */
export function messageExpiry(m: EphemeralMessage, chatTtlSeconds: number): number {
  if (m.viewOnce) return m.opened ? 0 : Infinity;
  if (m.selfDestructTtl && m.selfDestructTtl > 0) {
    return m.readMs === undefined ? Infinity : expiryAt(m.readMs, m.selfDestructTtl);
  }
  return expiryAt(m.sentMs, chatTtlSeconds);
}

export function isExpired(m: EphemeralMessage, chatTtlSeconds: number, now: number): boolean {
  return now >= messageExpiry(m, chatTtlSeconds);
}

/** sweepExpired returns the ids of messages that should be deleted now. */
export function sweepExpired(msgs: EphemeralMessage[], chatTtlSeconds: number, now: number): string[] {
  return msgs.filter((m) => isExpired(m, chatTtlSeconds, now)).map((m) => m.id);
}

/** nextExpiry is the soonest future expiry across `msgs` (to schedule a sweep),
 *  or Infinity when nothing is pending. */
export function nextExpiry(msgs: EphemeralMessage[], chatTtlSeconds: number, now: number): number {
  let soonest = Infinity;
  for (const m of msgs) {
    const e = messageExpiry(m, chatTtlSeconds);
    if (e > now && e < soonest) soonest = e;
  }
  return soonest;
}

// ── sealed control message: the per-chat timer, propagated between clients ────

export interface TimerControl {
  t: "timer";
  s: number; // ttl seconds (0 = off)
}

/** encodeTimerControl builds the sealed body for a "timer changed" control
 *  message (rides the E2EE envelope like polls/location). */
export function encodeTimerControl(seconds: number): string {
  return JSON.stringify({ t: "timer", s: Math.max(0, Math.floor(seconds)) });
}

/** parseTimerControl reads a decrypted control body, or null if it isn't one. */
export function parseTimerControl(body: string): TimerControl | null {
  try {
    const o = JSON.parse(body) as { t?: unknown; s?: unknown };
    if (o && o.t === "timer" && typeof o.s === "number" && isValidTtl(o.s)) {
      return { t: "timer", s: o.s };
    }
  } catch {
    /* not JSON / not a control message */
  }
  return null;
}

// ── per-message ephemeral flags, packed onto a text/media body ────────────────

export interface EphemeralFlags {
  /** self-destruct ttl in seconds (counts from read) */
  ttl?: number;
  /** view-once media */
  viewOnce?: boolean;
}

/** readEphemeralFlags extracts self-destruct/view-once flags from a decrypted
 *  body object (defensive: ignores malformed values). */
export function readEphemeralFlags(body: { ttl?: unknown; viewOnce?: unknown } | null | undefined): EphemeralFlags {
  const out: EphemeralFlags = {};
  if (body && typeof body.ttl === "number" && body.ttl > 0 && isValidTtl(body.ttl)) out.ttl = body.ttl;
  if (body && body.viewOnce === true) out.viewOnce = true;
  return out;
}
