import { describe, it, expect } from "vitest";
import { Cursors } from "./cursors";

describe("Cursors", () => {
  it("advances only forward; replays never rewind", () => {
    const c = new Cursors();
    expect(c.get("x")).toBe(0);
    expect(c.advance("x", 5)).toBe(true);
    expect(c.get("x")).toBe(5);
    expect(c.advance("x", 3)).toBe(false); // out-of-order / duplicate
    expect(c.get("x")).toBe(5);
    expect(c.advance("x", 6)).toBe(true);
    expect(c.get("x")).toBe(6);
  });

  it("detects a gap requiring SyncPull", () => {
    const c = new Cursors();
    c.advance("x", 5);
    expect(c.gapBefore("x", 6)).toBe(false); // contiguous
    expect(c.gapBefore("x", 5)).toBe(false); // duplicate
    expect(c.gapBefore("x", 8)).toBe(true); // missing 6,7
  });

  it("round-trips through snapshot/load", () => {
    const c = new Cursors();
    c.advance("a", 3);
    c.advance("b", 7);
    const restored = new Cursors();
    restored.load(c.snapshot());
    expect(restored.get("a")).toBe(3);
    expect(restored.get("b")).toBe(7);
  });
});
