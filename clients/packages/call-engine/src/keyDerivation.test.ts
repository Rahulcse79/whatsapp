import { describe, expect, it } from "vitest";
import { deriveFrameKey } from "./keyDerivation";

const secret = new Uint8Array(32).fill(0x5a);

describe("deriveFrameKey", () => {
  it("is deterministic and 32 bytes", async () => {
    const ctx = { roomId: "call-1", epoch: 0, senderId: "alice" };
    const a = await deriveFrameKey(secret, ctx);
    const b = await deriveFrameKey(secret, ctx);
    expect(a).toEqual(b);
    expect(a.length).toBe(32);
  });

  it("differs across room, epoch, and sender", async () => {
    const base = await deriveFrameKey(secret, { roomId: "call-1", epoch: 0, senderId: "alice" });
    const otherRoom = await deriveFrameKey(secret, { roomId: "call-2", epoch: 0, senderId: "alice" });
    const otherEpoch = await deriveFrameKey(secret, { roomId: "call-1", epoch: 1, senderId: "alice" });
    const otherSender = await deriveFrameKey(secret, { roomId: "call-1", epoch: 0, senderId: "bob" });
    expect(otherRoom).not.toEqual(base);
    expect(otherEpoch).not.toEqual(base);
    expect(otherSender).not.toEqual(base);
  });

  it("differs under a different root secret", async () => {
    const ctx = { roomId: "call-1", epoch: 0, senderId: "alice" };
    const a = await deriveFrameKey(secret, ctx);
    const b = await deriveFrameKey(new Uint8Array(32).fill(0x11), ctx);
    expect(a).not.toEqual(b);
  });
});
