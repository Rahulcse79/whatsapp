import { describe, expect, it } from "vitest";
import { decodePayload, encodeDirect, encodeSelfSync } from "./syncPayload";

const enc = (s: string) => new TextEncoder().encode(s);
const dec = (b: Uint8Array) => new TextDecoder().decode(b);

describe("syncPayload", () => {
  it("round-trips a direct copy (no sentTo)", () => {
    const p = decodePayload(encodeDirect(enc("hi bob")));
    expect(p.selfSync).toBe(false);
    expect(p.sentTo).toBeUndefined();
    expect(dec(p.content)).toBe("hi bob");
  });

  it("round-trips a self-sync copy carrying the recipient", () => {
    const p = decodePayload(encodeSelfSync("bob", enc("hi bob")));
    expect(p.selfSync).toBe(true);
    expect(p.sentTo).toBe("bob");
    expect(dec(p.content)).toBe("hi bob");
  });

  it("handles empty content and unicode recipients", () => {
    expect(decodePayload(encodeDirect(new Uint8Array(0))).content).toEqual(new Uint8Array(0));
    const p = decodePayload(encodeSelfSync("👤user", enc("x")));
    expect(p.sentTo).toBe("👤user");
    expect(dec(p.content)).toBe("x");
  });

  it("rejects a bad header or truncated self-sync frame", () => {
    expect(() => decodePayload(new Uint8Array([9, 0]))).toThrow(/bad header/); // wrong version
    expect(() => decodePayload(new Uint8Array([1, 1, 0, 5, 65]))).toThrow(/truncated sentTo/); // len 5, only 1 byte
    expect(() => decodePayload(new Uint8Array([1, 7]))).toThrow(/unknown kind/);
  });
});
