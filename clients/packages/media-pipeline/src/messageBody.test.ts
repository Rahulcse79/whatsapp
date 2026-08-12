import { describe, expect, it } from "vitest";
import type { MediaEnvelope } from "./envelope";
import { encodeMediaMessage, parseMediaMessage } from "./messageBody";

const env: MediaEnvelope = {
  objectKey: "media/x",
  fileKey: "a2V5",
  contentHash: "aGFzaA==",
  sizeBytes: 1234,
  mime: "image/webp",
};

describe("encode/parseMediaMessage", () => {
  it("round-trips attachments and caption", () => {
    const body = encodeMediaMessage([env], "look at this");
    const parsed = parseMediaMessage(body);
    expect(parsed).toEqual({ attachments: [env], caption: "look at this" });
  });

  it("omits an empty caption", () => {
    const parsed = parseMediaMessage(encodeMediaMessage([env], "   "));
    expect(parsed).toEqual({ attachments: [env] });
  });

  it("treats plain text and malformed bodies as non-media (null)", () => {
    expect(parseMediaMessage("hello there")).toBeNull();
    expect(parseMediaMessage("")).toBeNull();
    expect(parseMediaMessage("{not json")).toBeNull();
    expect(parseMediaMessage(JSON.stringify({ t: "text" }))).toBeNull();
    expect(parseMediaMessage(JSON.stringify({ t: "media", a: [{ objectKey: "x" }] }))).toBeNull(); // incomplete envelope
  });
});
