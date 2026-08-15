import { describe, expect, it } from "vitest";
import type { MediaEnvelope } from "./envelope";
import type { LinkPreview } from "./linkPreview";
import { encodeMediaMessage, encodePoll, encodeReaction, encodeSticker, encodeTextMessage, parseMediaMessage, parsePoll, parseReaction, parseTextMessage } from "./messageBody";

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

describe("encode/parseTextMessage (link previews, FR-MSG-08)", () => {
  const preview: LinkPreview = { url: "https://example.com/final", title: "Hello", description: "world", siteName: "Example" };

  it("keeps a preview-less text message as a bare string (back-compat)", () => {
    expect(encodeTextMessage("just text")).toBe("just text");
    expect(parseTextMessage("just text")).toEqual({ text: "just text" });
  });

  it("round-trips text plus a link preview", () => {
    const body = encodeTextMessage("look https://example.com", preview);
    expect(body.charAt(0)).toBe("{"); // tagged object, sealed as the body
    expect(parseTextMessage(body)).toEqual({ text: "look https://example.com", linkPreview: preview });
  });

  it("round-trips a quoted reply (FR-MSG-04)", () => {
    const reply = { msgUuid: "abc", snippet: "the original", mine: false };
    const body = encodeTextMessage("my reply", undefined, reply);
    expect(body.charAt(0)).toBe("{"); // reply forces the tagged form
    const parsed = parseTextMessage(body);
    expect(parsed.text).toBe("my reply");
    expect(parsed.reply).toEqual(reply);
  });

  it("reads plain and malformed bodies as raw text (never throws)", () => {
    expect(parseTextMessage("")).toEqual({ text: "" });
    expect(parseTextMessage("{not json")).toEqual({ text: "{not json" });
    expect(parseTextMessage(JSON.stringify({ t: "text", text: "hi", lp: { url: "u" } }))).toEqual({ text: "hi" }); // lp missing title → dropped
    // A media body is not a text body — parseMediaMessage handles it; here it reads as its raw JSON text.
    const media = encodeMediaMessage([env]);
    expect(parseTextMessage(media)).toEqual({ text: media });
  });
});

describe("encode/parseReaction (T5.05b)", () => {
  it("round-trips an add reaction", () => {
    expect(parseReaction(encodeReaction("👍", "add"))).toEqual({ emoji: "👍", op: "add" });
  });
  it("round-trips a remove reaction", () => {
    expect(parseReaction(encodeReaction("❤️", "remove"))).toEqual({ emoji: "❤️", op: "remove" });
  });
  it("defaults an unknown op to add", () => {
    expect(parseReaction(JSON.stringify({ t: "react", emoji: "😮", op: "bogus" }))).toEqual({ emoji: "😮", op: "add" });
  });
  it("returns null for non-reaction bodies", () => {
    expect(parseReaction("just text")).toBeNull();
    expect(parseReaction(encodeTextMessage("hi"))).toBeNull();
    expect(parseReaction("{not json")).toBeNull();
  });
});

describe("encode/parsePoll (T6.02)", () => {
  it("round-trips a poll body", () => {
    const body = encodePoll("p1", "Lunch?", ["Pizza", "Sushi"], true);
    expect(parsePoll(body)).toEqual({ pollId: "p1", question: "Lunch?", options: ["Pizza", "Sushi"], multi: true });
  });
  it("defaults multi to false and returns null for non-poll bodies", () => {
    expect(parsePoll(encodePoll("p2", "Q", ["a", "b"], false))?.multi).toBe(false);
    expect(parsePoll("just text")).toBeNull();
    expect(parsePoll(encodeSticker("k", "😀"))).toBeNull();
  });
});
