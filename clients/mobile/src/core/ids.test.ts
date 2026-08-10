import { describe, expect, it } from "vitest";
import { isValidPhone, newId } from "./ids";

describe("newId", () => {
  it("is deterministic under injected now/rng", () => {
    const a = newId(() => 1000, () => 0);
    const b = newId(() => 1000, () => 0);
    expect(a).toBe(b);
  });

  it("sorts lexicographically by creation time", () => {
    const earlier = newId(() => 1000, () => 0.1);
    const later = newId(() => 2_000_000, () => 0.1);
    expect(earlier < later).toBe(true);
  });
});

describe("isValidPhone", () => {
  it("accepts E.164 and rejects junk", () => {
    expect(isValidPhone("+14155550123")).toBe(true);
    expect(isValidPhone(" +919812345678 ")).toBe(true);
    expect(isValidPhone("14155550123")).toBe(false); // no +
    expect(isValidPhone("+0123")).toBe(false); // leading 0 / too short
    expect(isValidPhone("hello")).toBe(false);
  });
});
