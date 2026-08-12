import { describe, expect, it } from "vitest";
import { encodeLinkQr, parseLinkQr } from "./qrLink";

describe("encode/parseLinkQr", () => {
  const payload = { linkToken: "lt_abc123", deviceId: "dev-9", identityKey: new Uint8Array([0, 1, 2, 250, 255]) };

  it("round-trips the link token, device id, and identity key", () => {
    const parsed = parseLinkQr(encodeLinkQr(payload));
    expect(parsed).not.toBeNull();
    expect(parsed?.linkToken).toBe("lt_abc123");
    expect(parsed?.deviceId).toBe("dev-9");
    expect(Array.from(parsed!.identityKey)).toEqual([0, 1, 2, 250, 255]);
  });

  it("returns null on malformed / non-JSON input", () => {
    expect(parseLinkQr("not json")).toBeNull();
    expect(parseLinkQr("[]")).toBeNull();
    expect(parseLinkQr("42")).toBeNull();
  });

  it("returns null on a wrong version or missing fields", () => {
    expect(parseLinkQr(JSON.stringify({ v: 2, t: "x", d: "y", k: "00" }))).toBeNull();
    expect(parseLinkQr(JSON.stringify({ v: 1, t: "x", d: "y" }))).toBeNull(); // no key
    expect(parseLinkQr(JSON.stringify({ v: 1, t: "", d: "y", k: "00" }))).toBeNull(); // empty token
  });

  it("returns null on a bad hex identity key", () => {
    expect(parseLinkQr(JSON.stringify({ v: 1, t: "x", d: "y", k: "zz" }))).toBeNull();
    expect(parseLinkQr(JSON.stringify({ v: 1, t: "x", d: "y", k: "abc" }))).toBeNull(); // odd length
  });
});
