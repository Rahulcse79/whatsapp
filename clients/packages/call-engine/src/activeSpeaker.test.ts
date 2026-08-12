import { describe, expect, it } from "vitest";
import { ActiveSpeakerTracker, type AudioLevel } from "./activeSpeaker";

const lv = (m: Record<string, number>): AudioLevel[] =>
  Object.entries(m).map(([participantId, level]) => ({ participantId, level }));

describe("ActiveSpeakerTracker", () => {
  it("picks the loudest participant above threshold", () => {
    const t = new ActiveSpeakerTracker({ threshold: 0.15 });
    expect(t.update(lv({ a: 0.1, b: 0.05 }), 0)).toBeNull(); // nobody above threshold
    expect(t.update(lv({ a: 0.4, b: 0.2 }), 100)).toBe("a");
  });

  it("holds focus on the current speaker while it stays loud (no thrash)", () => {
    const t = new ActiveSpeakerTracker({ threshold: 0.15 });
    expect(t.update(lv({ a: 0.5, b: 0.1 }), 0)).toBe("a");
    // b is briefly louder, but a is still speaking → focus holds on a.
    expect(t.update(lv({ a: 0.3, b: 0.6 }), 100)).toBe("a");
  });

  it("switches once the current speaker drops below threshold", () => {
    const t = new ActiveSpeakerTracker({ threshold: 0.15 });
    expect(t.update(lv({ a: 0.5 }), 0)).toBe("a");
    expect(t.update(lv({ a: 0.05, b: 0.4 }), 100)).toBe("b"); // a quiet → b takes over
  });

  it("holds through brief silence, then clears after holdMs", () => {
    const t = new ActiveSpeakerTracker({ threshold: 0.15, holdMs: 1000 });
    expect(t.update(lv({ a: 0.5 }), 0)).toBe("a");
    expect(t.update(lv({ a: 0.0 }), 500)).toBe("a"); // silent, but within hold
    expect(t.update(lv({ a: 0.0 }), 1600)).toBeNull(); // held long enough → cleared
  });

  it("yields focus when the current speaker leaves the call", () => {
    const t = new ActiveSpeakerTracker({ threshold: 0.15 });
    expect(t.update(lv({ a: 0.5, b: 0.2 }), 0)).toBe("a");
    // a is gone from the report and b is speaking → b takes focus.
    expect(t.update(lv({ b: 0.4 }), 100)).toBe("b");
  });
});
