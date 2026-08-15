import { describe, expect, it } from "vitest";
import { openForConversation, sealForConversation } from "./convCrypto";

describe("convCrypto (dev double)", () => {
  it("round-trips a message for its conversation", () => {
    const env = sealForConversation("conv-1", "hello 👋");
    expect(openForConversation("conv-1", env)).toBe("hello 👋");
  });

  it("two devices sharing the conversation id interoperate", () => {
    // Sender seals; a fresh open() (the recipient) with the same conversation id
    // recovers the plaintext — this is what makes 1:1 messaging work.
    const env = sealForConversation("shared-conv", "meet at 6");
    expect(openForConversation("shared-conv", env)).toBe("meet at 6");
  });

  it("a different conversation id cannot open it (MAC fails)", () => {
    const env = sealForConversation("conv-a", "secret");
    expect(() => openForConversation("conv-b", env)).toThrow();
  });

  it("a per-message nonce varies the ciphertext but not the plaintext", () => {
    const a = sealForConversation("c", "same");
    const b = sealForConversation("c", "same");
    expect(Buffer.from(a).toString("hex")).not.toBe(Buffer.from(b).toString("hex"));
    expect(openForConversation("c", a)).toBe("same");
    expect(openForConversation("c", b)).toBe("same");
  });

  it("handles empty, unicode, and long bodies", () => {
    for (const s of ["", "a", "长文本".repeat(100), "emoji 🎉🔥 and ascii"]) {
      expect(openForConversation("c", sealForConversation("c", s))).toBe(s);
    }
  });

  it("rejects a truncated/tampered envelope", () => {
    const env = sealForConversation("c", "hello");
    expect(() => openForConversation("c", env.subarray(0, 10))).toThrow();
    const tampered = env.slice();
    tampered[tampered.length - 1] ^= 0xff;
    expect(() => openForConversation("c", tampered)).toThrow();
  });
});
