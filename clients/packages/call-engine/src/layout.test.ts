import { describe, expect, it } from "vitest";
import { computeLayout, desiredReceiveLayers } from "./layout";

describe("computeLayout", () => {
  const people = ["a", "b", "c", "d", "e"];

  it("focuses the active speaker and sorts it first", () => {
    const layout = computeLayout(people, "c", 9);
    expect(layout.tiles[0]).toEqual({ participantId: "c", focused: true });
    expect(layout.tiles.filter((t) => t.focused)).toHaveLength(1);
    expect(layout.tiles).toHaveLength(5);
  });

  it("prefers a pinned participant over the active speaker", () => {
    const layout = computeLayout(people, "c", 9, "e");
    expect(layout.tiles[0]).toEqual({ participantId: "e", focused: true });
  });

  it("caps the grid at maxTiles (extras stay off-grid)", () => {
    const layout = computeLayout(people, "d", 3);
    expect(layout.tiles.map((t) => t.participantId)).toEqual(["d", "a", "b"]); // focus + first two
    expect(layout.tiles).toHaveLength(3);
  });

  it("has no focused tile when nobody is speaking or pinned", () => {
    const layout = computeLayout(people, null, 9);
    expect(layout.tiles.every((t) => !t.focused)).toBe(true);
  });
});

describe("desiredReceiveLayers", () => {
  const people = ["a", "b", "c", "d"];

  it("gives the focused tile the top layer, grid tiles reduced, off-grid audio-only", () => {
    const layout = computeLayout(people, "a", 3); // shows a(focused), b, c; d off-grid
    const layers = desiredReceiveLayers(people, layout, 1500 /* good downlink */);
    expect(layers.get("a")).toBe("f"); // focused + few tiles + good link
    expect(layers.get("b")).toBe("h"); // grid tile
    expect(layers.get("c")).toBe("h");
    expect(layers.get("d")).toBe("audio-only"); // not in the layout
  });

  it("drops everyone to audio-only below the video floor", () => {
    const layout = computeLayout(people, "a", 4);
    const layers = desiredReceiveLayers(people, layout, 50 /* below the 80 kbps floor */);
    expect([...layers.values()].every((v) => v === "audio-only")).toBe(true);
  });
});
