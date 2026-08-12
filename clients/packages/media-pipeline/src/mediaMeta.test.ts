import { describe, expect, it } from "vitest";
import type { MediaEnvelope } from "./envelope";
import { classifyMedia, downloadName, formatBytes, formatDuration, guessExtension, isVoiceNote } from "./mediaMeta";

const env = (over: Partial<MediaEnvelope>): MediaEnvelope => ({
  objectKey: "k",
  fileKey: "",
  contentHash: "",
  sizeBytes: 0,
  mime: "application/octet-stream",
  ...over,
});

describe("classifyMedia", () => {
  it("buckets by MIME major type, defaulting to document", () => {
    expect(classifyMedia("image/webp")).toBe("image");
    expect(classifyMedia("IMAGE/PNG")).toBe("image");
    expect(classifyMedia("video/mp4")).toBe("video");
    expect(classifyMedia("audio/ogg; codecs=opus")).toBe("audio");
    expect(classifyMedia("application/pdf")).toBe("document");
    expect(classifyMedia("text/plain")).toBe("document");
  });
});

describe("isVoiceNote", () => {
  it("is true only for audio explicitly flagged as a voice note", () => {
    expect(isVoiceNote(env({ mime: "audio/ogg", voice: true }))).toBe(true);
    expect(isVoiceNote(env({ mime: "audio/ogg" }))).toBe(false);
    expect(isVoiceNote(env({ mime: "video/mp4", voice: true }))).toBe(false);
  });
});

describe("formatBytes", () => {
  it("renders human sizes with one decimal below 10 units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(812)).toBe("812 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1_258_291)).toBe("1.2 MB");
    expect(formatBytes(25 * 1024 * 1024)).toBe("25 MB");
    expect(formatBytes(-5)).toBe("0 B");
  });
});

describe("formatDuration", () => {
  it("renders m:ss and h:mm:ss", () => {
    expect(formatDuration(0)).toBe("0:00");
    expect(formatDuration(7_000)).toBe("0:07");
    expect(formatDuration(83_000)).toBe("1:23");
    expect(formatDuration(3_725_000)).toBe("1:02:05");
  });
});

describe("guessExtension / downloadName", () => {
  it("maps known MIME types and falls back to bin", () => {
    expect(guessExtension("image/jpeg")).toBe("jpg");
    expect(guessExtension("application/pdf")).toBe("pdf");
    expect(guessExtension("audio/ogg; codecs=opus")).toBe("ogg");
    expect(guessExtension("application/x-unknown")).toBe("bin");
  });

  it("prefers the sender filename, else synthesizes media.<ext>", () => {
    expect(downloadName(env({ mime: "application/pdf", filename: "invoice.pdf" }))).toBe("invoice.pdf");
    expect(downloadName(env({ mime: "image/png" }))).toBe("media.png");
    expect(downloadName(env({ mime: "image/png", filename: "   " }))).toBe("media.png");
  });
});
