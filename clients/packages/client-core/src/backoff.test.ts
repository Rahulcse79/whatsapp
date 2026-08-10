import { describe, expect, it } from "vitest";
import { backoffDelay, defaultBackoff } from "./backoff";

describe("backoffDelay", () => {
  it("returns 0 at the low end and the full ceiling at the high end of the jitter", () => {
    expect(backoffDelay(0, defaultBackoff, () => 0)).toBe(0);
    expect(backoffDelay(0, defaultBackoff, () => 1)).toBe(500); // base·2^0
    expect(backoffDelay(0, defaultBackoff, () => 0.5)).toBe(250);
  });

  it("grows the exponential ceiling per attempt", () => {
    const full = (attempt: number) => backoffDelay(attempt, defaultBackoff, () => 1);
    expect(full(1)).toBe(1000);
    expect(full(2)).toBe(2000);
    expect(full(4)).toBe(8000);
  });

  it("clamps the ceiling at capMs", () => {
    // 500·2^10 = 512000, well past the 30 s cap.
    expect(backoffDelay(10, defaultBackoff, () => 1)).toBe(30_000);
    expect(backoffDelay(100, defaultBackoff, () => 1)).toBe(30_000);
  });
});
