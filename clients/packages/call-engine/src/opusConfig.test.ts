import { describe, expect, it } from "vitest";
import { applyOpusConfig, buildOpusParams, OPUS_MAX_KBPS, OPUS_MIN_KBPS } from "./opusConfig";

const sdp = ["v=0", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=rtpmap:111 opus/48000/2", "a=fmtp:111 minptime=10"].join("\r\n");

describe("buildOpusParams", () => {
  it("clamps the bitrate to the 6–32 kbps voice band and enables DTX+FEC", () => {
    expect(buildOpusParams(100)).toEqual({ dtx: true, fec: true, maxAverageBitrateBps: OPUS_MAX_KBPS * 1000 });
    expect(buildOpusParams(1).maxAverageBitrateBps).toBe(OPUS_MIN_KBPS * 1000);
    expect(buildOpusParams(24).maxAverageBitrateBps).toBe(24000);
  });
});

describe("applyOpusConfig", () => {
  it("rewrites the opus fmtp with DTX+FEC+bitrate, preserving other params", () => {
    const out = applyOpusConfig(sdp, buildOpusParams(24));
    expect(out).toContain("usedtx=1");
    expect(out).toContain("useinbandfec=1");
    expect(out).toContain("maxaveragebitrate=24000");
    expect(out).toContain("minptime=10"); // untouched
  });

  it("inserts an fmtp line right after the rtpmap when none exists", () => {
    const noFmtp = ["m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=rtpmap:111 opus/48000/2"].join("\r\n");
    const out = applyOpusConfig(noFmtp, buildOpusParams());
    expect(out).toContain("a=fmtp:111 useinbandfec=1;usedtx=1;maxaveragebitrate=32000");
  });

  it("leaves an SDP without Opus unchanged", () => {
    const video = "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 VP8/90000";
    expect(applyOpusConfig(video, buildOpusParams())).toBe(video);
  });
});
