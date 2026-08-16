import { describe, expect, it } from "vitest";
import {
  DISAPPEARING_PRESETS,
  encodeTimerControl,
  expiryAt,
  isExpired,
  isValidTtl,
  messageExpiry,
  nextExpiry,
  parseTimerControl,
  readEphemeralFlags,
  sweepExpired,
  type EphemeralMessage,
} from "./ephemeral";

const T0 = 1_000_000_000; // fixed "sent" clock

describe("timer validation + presets", () => {
  it("bounds the ttl", () => {
    expect(isValidTtl(0)).toBe(true);
    expect(isValidTtl(86_400)).toBe(true);
    expect(isValidTtl(-1)).toBe(false);
    expect(isValidTtl(7_776_001)).toBe(false);
  });
  it("offers an Off preset", () => {
    expect(DISAPPEARING_PRESETS[0]).toEqual({ seconds: 0, label: "Off" });
  });
});

describe("expiry math", () => {
  it("chat timer counts from sent; off = never", () => {
    expect(expiryAt(T0, 60)).toBe(T0 + 60_000);
    expect(expiryAt(T0, 0)).toBe(Infinity);
  });

  it("self-destruct counts from read and stays until read", () => {
    const m: EphemeralMessage = { id: "m", sentMs: T0, selfDestructTtl: 10 };
    expect(messageExpiry(m, 0)).toBe(Infinity); // unread
    const read: EphemeralMessage = { ...m, readMs: T0 + 5_000 };
    expect(messageExpiry(read, 0)).toBe(T0 + 15_000);
  });

  it("view-once is gone the instant it's opened", () => {
    const m: EphemeralMessage = { id: "m", sentMs: T0, viewOnce: true };
    expect(messageExpiry(m, 0)).toBe(Infinity);
    expect(messageExpiry({ ...m, opened: true }, 0)).toBe(0);
  });

  it("self-destruct overrides the chat timer", () => {
    const m: EphemeralMessage = { id: "m", sentMs: T0, readMs: T0, selfDestructTtl: 5 };
    expect(messageExpiry(m, 86_400)).toBe(T0 + 5_000); // not the 24h chat timer
  });
});

describe("sweep", () => {
  const msgs: EphemeralMessage[] = [
    { id: "old", sentMs: T0 },
    { id: "fresh", sentMs: T0 + 50_000 },
    { id: "once", sentMs: T0, viewOnce: true, opened: true },
  ];
  it("collects expired ids under a chat timer + opened view-once", () => {
    const now = T0 + 60_000;
    const gone = sweepExpired(msgs, 30, now); // 30s chat timer
    expect(gone.sort()).toEqual(["old", "once"]);
  });
  it("nextExpiry finds the soonest pending", () => {
    const now = T0 + 10_000;
    // old expired already (excluded); fresh expires at T0+50k+30k
    expect(nextExpiry([{ id: "fresh", sentMs: T0 + 50_000 }], 30, now)).toBe(T0 + 80_000);
    expect(nextExpiry([{ id: "off", sentMs: T0 }], 0, now)).toBe(Infinity);
  });
  it("isExpired matches messageExpiry", () => {
    expect(isExpired({ id: "x", sentMs: T0 }, 30, T0 + 40_000)).toBe(true);
    expect(isExpired({ id: "x", sentMs: T0 }, 30, T0 + 10_000)).toBe(false);
  });
});

describe("timer control message", () => {
  it("round-trips", () => {
    expect(parseTimerControl(encodeTimerControl(86_400))).toEqual({ t: "timer", s: 86_400 });
    expect(encodeTimerControl(30.9)).toBe('{"t":"timer","s":30}'); // floored
  });
  it("rejects non-control / out-of-range bodies", () => {
    expect(parseTimerControl('{"t":"text","text":"hi"}')).toBeNull();
    expect(parseTimerControl("not json")).toBeNull();
    expect(parseTimerControl('{"t":"timer","s":-5}')).toBeNull();
  });
});

describe("per-message flags", () => {
  it("reads self-destruct + view-once, ignoring junk", () => {
    expect(readEphemeralFlags({ ttl: 30, viewOnce: true })).toEqual({ ttl: 30, viewOnce: true });
    expect(readEphemeralFlags({ ttl: -1 })).toEqual({});
    expect(readEphemeralFlags({ viewOnce: "yes" as unknown as boolean })).toEqual({});
    expect(readEphemeralFlags(null)).toEqual({});
  });
});
