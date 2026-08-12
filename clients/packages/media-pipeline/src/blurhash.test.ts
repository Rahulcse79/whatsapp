import { describe, expect, it } from "vitest";
import { blurhashAverageColor, blurhashCssColor, decodeBlurhash } from "./blurhash";

// Canonical example from the BlurHash reference (a 4×3-component hash).
const SAMPLE = "LEHV6nWB2yk8pyo0adR*.7kCMdnj";

describe("decodeBlurhash", () => {
  it("rasterizes to width×height RGBA with opaque alpha", () => {
    const w = 8;
    const h = 6;
    const px = decodeBlurhash(SAMPLE, w, h);
    expect(px.length).toBe(w * h * 4);
    for (let i = 3; i < px.length; i += 4) expect(px[i]).toBe(255); // alpha
    for (let i = 0; i < px.length; i++) {
      expect(px[i]!).toBeGreaterThanOrEqual(0);
      expect(px[i]!).toBeLessThanOrEqual(255);
    }
  });

  it("throws on a malformed hash", () => {
    expect(() => decodeBlurhash("!!", 4, 4)).toThrow();
    expect(() => decodeBlurhash(SAMPLE.slice(0, 10), 4, 4)).toThrow(/length/);
  });
});

describe("blurhashAverageColor / blurhashCssColor", () => {
  it("returns an in-range DC colour", () => {
    const { r, g, b } = blurhashAverageColor(SAMPLE);
    for (const c of [r, g, b]) {
      expect(c).toBeGreaterThanOrEqual(0);
      expect(c).toBeLessThanOrEqual(255);
    }
  });

  it("formats a CSS colour and falls back to grey when absent/invalid", () => {
    expect(blurhashCssColor(SAMPLE)).toMatch(/^rgb\(\d+,\d+,\d+\)$/);
    expect(blurhashCssColor(undefined)).toBe("rgb(200,200,200)");
    expect(blurhashCssColor("nope")).toBe("rgb(200,200,200)");
  });
});
