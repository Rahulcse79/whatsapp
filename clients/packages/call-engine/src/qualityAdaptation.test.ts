import { describe, expect, it } from "vitest";
import { QualityController } from "./qualityAdaptation";

describe("QualityController", () => {
  it("drops video below the floor and restores only past the higher threshold", () => {
    const c = new QualityController();
    expect(c.update({ availableKbps: 800, packetLoss: 0 }).video).toBe(true);
    expect(c.update({ availableKbps: 90, packetLoss: 0 }).video).toBe(false); // < 100 floor → audio-only
    expect(c.update({ availableKbps: 150, packetLoss: 0 }).video).toBe(false); // hysteresis: < 250 restore
    expect(c.update({ availableKbps: 300, packetLoss: 0 }).video).toBe(true); // ≥ 250 → video back
  });

  it("adapts audio bitrate within the Opus band as loss rises", () => {
    const c = new QualityController();
    expect(c.update({ availableKbps: 500, packetLoss: 0 }).audioKbps).toBe(32); // top of band
    expect(c.update({ availableKbps: 500, packetLoss: 1 }).audioKbps).toBe(6); // full loss → floor
  });

  it("reports audio-only quality (no video bitrate) once video is dropped", () => {
    const c = new QualityController();
    expect(c.update({ availableKbps: 50, packetLoss: 0 })).toMatchObject({ video: false, videoKbps: 0 });
  });

  it("notifies the listener with the adapted quality", () => {
    const seen: boolean[] = [];
    const c = new QualityController((q) => seen.push(q.video));
    c.update({ availableKbps: 800, packetLoss: 0 });
    c.update({ availableKbps: 40, packetLoss: 0 });
    expect(seen).toEqual([true, false]);
  });
});
