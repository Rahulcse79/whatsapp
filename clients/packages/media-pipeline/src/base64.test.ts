import { describe, expect, it } from "vitest";
import { fromBase64, toBase64 } from "./base64";

const enc = (s: string) => Uint8Array.from(s, (c) => c.charCodeAt(0));

describe("base64", () => {
  // RFC 4648 §10 vectors — must match Go's encoding/base64.StdEncoding byte for
  // byte, since the wire contract (content_hash, envelope key) crosses to the Go
  // media-svc.
  const vectors: Array<[string, string]> = [
    ["", ""],
    ["f", "Zg=="],
    ["fo", "Zm8="],
    ["foo", "Zm9v"],
    ["foob", "Zm9vYg=="],
    ["fooba", "Zm9vYmE="],
    ["foobar", "Zm9vYmFy"],
  ];

  it("matches RFC 4648 encode vectors", () => {
    for (const [plain, b64] of vectors) {
      expect(toBase64(enc(plain))).toBe(b64);
    }
  });

  it("matches RFC 4648 decode vectors", () => {
    for (const [plain, b64] of vectors) {
      expect(fromBase64(b64)).toEqual(enc(plain));
    }
  });

  it("round-trips arbitrary binary including high bytes and zeros", () => {
    const raw = new Uint8Array(513);
    for (let i = 0; i < raw.length; i++) raw[i] = (i * 251) & 0xff;
    expect(fromBase64(toBase64(raw))).toEqual(raw);
  });

  it("round-trips a 32-byte digest-shaped value (the common case)", () => {
    const hash = crypto.getRandomValues(new Uint8Array(32));
    const b64 = toBase64(hash);
    expect(b64).toHaveLength(44); // 32 bytes ≡ 2 (mod 3) → 44 chars, single '=' pad
    expect(b64.endsWith("=")).toBe(true);
    expect(b64.endsWith("==")).toBe(false);
    expect(fromBase64(b64)).toEqual(hash);
  });

  it("tolerates surrounding whitespace/newlines on decode", () => {
    expect(fromBase64("Zm9v\nYmFy\n")).toEqual(enc("foobar"));
  });
});
