import { describe, expect, it } from "vitest";
import { b64urlToBytes, bytesToB64url, shouldRelock } from "./deviceAuth";

describe("base64url codec", () => {
  it("round-trips arbitrary bytes", () => {
    for (const arr of [[], [0], [255, 0, 128, 1], [1, 2, 3, 4, 5]]) {
      const bytes = new Uint8Array(arr);
      expect([...b64urlToBytes(bytesToB64url(bytes))]).toEqual(arr);
    }
  });
  it("uses the url alphabet with no padding", () => {
    const s = bytesToB64url(new Uint8Array([251, 255, 191]));
    expect(s).not.toMatch(/[+/=]/);
  });
  it("decodes a known vector", () => {
    // "hi" → base64url "aGk"
    expect(bytesToB64url(new Uint8Array([104, 105]))).toBe("aGk");
    expect([...b64urlToBytes("aGk")]).toEqual([104, 105]);
  });
});

describe("app-lock idle policy", () => {
  it("locks immediately when timeout is 0", () => {
    expect(shouldRelock(1000, 0, 1000)).toBe(true);
  });
  it("relocks only after the timeout elapses", () => {
    const t0 = 1_000_000;
    expect(shouldRelock(t0, 60, t0 + 30_000)).toBe(false);
    expect(shouldRelock(t0, 60, t0 + 60_000)).toBe(true);
  });
});
