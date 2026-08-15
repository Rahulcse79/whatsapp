import { describe, expect, it } from "vitest";
import { panForTile, SpatialAudioController, type SpatialPanner } from "./spatialAudio";

describe("panForTile (T9.01)", () => {
  it("centres a single tile", () => {
    expect(panForTile(0, 1)).toBe(0);
  });
  it("spreads tiles left→right, scaled by width", () => {
    expect(panForTile(0, 3, 1)).toBe(-1); // leftmost
    expect(panForTile(1, 3, 1)).toBe(0); // middle
    expect(panForTile(2, 3, 1)).toBe(1); // rightmost
    expect(panForTile(0, 3, 0.5)).toBe(-0.5); // width-scaled
  });
  it("returns 0 for out-of-range", () => {
    expect(panForTile(5, 3)).toBe(0);
  });
});

function fakePanner(): SpatialPanner & { pans: Record<string, number>; released: string[] } {
  const pans: Record<string, number> = {};
  const released: string[] = [];
  return {
    pans,
    released,
    setPan: (id, pan) => {
      pans[id] = pan;
    },
    release: (id) => {
      released.push(id);
      delete pans[id];
    },
  };
}

describe("SpatialAudioController (T9.01)", () => {
  it("pans participants by their tile order and releases leavers", () => {
    const p = fakePanner();
    const c = new SpatialAudioController(p, { width: 1 });
    c.layout(["a", "b", "c"]);
    expect(p.pans).toEqual({ a: -1, b: 0, c: 1 });
    c.layout(["a", "c"]); // b left
    expect(p.released).toContain("b");
    expect(p.pans).toEqual({ a: -1, c: 1 });
  });

  it("re-centres everyone when disabled", () => {
    const p = fakePanner();
    const c = new SpatialAudioController(p, { width: 1 });
    c.layout(["a", "b"]);
    c.setEnabled(false);
    expect(p.pans).toEqual({ a: 0, b: 0 });
  });
});
