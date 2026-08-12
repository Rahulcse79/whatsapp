import { describe, expect, it } from "vitest";
import { AUDIO_ONLY_FLOOR_KBPS, buildSimulcastEncodings, chooseReceiveLayer, SIMULCAST_LADDER } from "./simulcast";

const active = (uplinkKbps?: number) =>
  buildSimulcastEncodings(uplinkKbps)
    .filter((e) => e.active)
    .map((e) => e.rid);

describe("SIMULCAST_LADDER", () => {
  it("is the rtc-lld §3 f/h/q ladder, descending bitrate", () => {
    expect(SIMULCAST_LADDER.map((l) => l.rid)).toEqual(["f", "h", "q"]);
    expect(SIMULCAST_LADDER.map((l) => l.maxBitrate)).toEqual([1_200_000, 400_000, 100_000]);
    expect(SIMULCAST_LADDER.map((l) => l.scaleResolutionDownBy)).toEqual([1, 2, 4]);
  });
});

describe("buildSimulcastEncodings", () => {
  it("publishes all three layers on an unconstrained uplink", () => {
    expect(active()).toEqual(["f", "h", "q"]);
  });

  it("admits layers bottom-up to fit the uplink budget", () => {
    // ~1.7 Mbps needed for all three; 600 kbps fits only q+h; 90 kbps only q.
    expect(active(1700)).toEqual(["f", "h", "q"]);
    expect(active(600)).toEqual(["h", "q"]);
    expect(active(120)).toEqual(["q"]);
  });

  it("always keeps q active even below its bitrate (floor)", () => {
    expect(active(10)).toEqual(["q"]);
    expect(buildSimulcastEncodings(10).find((e) => e.rid === "q")?.active).toBe(true);
  });
});

describe("chooseReceiveLayer", () => {
  it("drops to audio-only below the video floor", () => {
    expect(chooseReceiveLayer({ downlinkKbps: AUDIO_ONLY_FLOOR_KBPS - 1, tileCount: 1, focused: true })).toBe("audio-only");
  });

  it("gives full res to a focused tile on a good link with few tiles", () => {
    expect(chooseReceiveLayer({ downlinkKbps: 1500, tileCount: 2, focused: true })).toBe("f");
  });

  it("uses half res for a grid or an unfocused/moderate link", () => {
    expect(chooseReceiveLayer({ downlinkKbps: 1500, tileCount: 9, focused: true })).toBe("h"); // too many tiles for f
    expect(chooseReceiveLayer({ downlinkKbps: 500, tileCount: 1, focused: true })).toBe("h"); // link too weak for f
    expect(chooseReceiveLayer({ downlinkKbps: 1500, tileCount: 2, focused: false })).toBe("h"); // not focused
  });

  it("falls to quarter res on a poor (but above floor) link", () => {
    expect(chooseReceiveLayer({ downlinkKbps: 150, tileCount: 1, focused: true })).toBe("q");
  });
});
