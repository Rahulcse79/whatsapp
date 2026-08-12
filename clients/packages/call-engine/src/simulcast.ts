// Simulcast quality ladder (rtc-lld §3). A video sender publishes up to three
// spatial layers at once; the SFU forwards whichever a given receiver's link and
// layout can afford. This module is the pure policy: the ladder itself, which
// layers a constrained UPLINK should publish, and which layer a receiver should
// subscribe given its DOWNLINK + how many tiles it's showing. No WebRTC types —
// the platform transport maps RtpEncoding onto RTCRtpEncodingParameters.

/** Ladder rungs: f = full, h = half, q = quarter. */
export type LayerId = "f" | "h" | "q";

export interface SimulcastLayer {
  rid: LayerId;
  /** Target cap in bits per second. */
  maxBitrate: number;
  /** Downscale factor from the captured resolution (1 = native). */
  scaleResolutionDownBy: number;
  maxFramerate: number;
}

/** The rtc-lld §3 ladder: 720p/~1.2Mbps · 360p/~400kbps · 90–180p/~100kbps. */
export const SIMULCAST_LADDER: readonly SimulcastLayer[] = [
  { rid: "f", maxBitrate: 1_200_000, scaleResolutionDownBy: 1, maxFramerate: 30 },
  { rid: "h", maxBitrate: 400_000, scaleResolutionDownBy: 2, maxFramerate: 30 },
  { rid: "q", maxBitrate: 100_000, scaleResolutionDownBy: 4, maxFramerate: 15 },
];

/** Below this downlink, video is dropped and the call falls back to audio-only
 *  (rtc-lld §3: "below floor → auto audio-only"). */
export const AUDIO_ONLY_FLOOR_KBPS = 80;

/** RtpEncoding is one ladder rung plus whether the sender currently publishes it. */
export interface RtpEncoding extends SimulcastLayer {
  active: boolean;
}

/** buildSimulcastEncodings returns the three ladder rungs with `active` set to
 *  fit an uplink budget (kbps; omitted = unconstrained). Layers are admitted
 *  bottom-up so the total stays within budget; q (the floor) is always active —
 *  if even q won't fit, the receiver side drops to audio-only. */
export function buildSimulcastEncodings(uplinkKbps?: number): RtpEncoding[] {
  const budget = uplinkKbps === undefined ? Number.POSITIVE_INFINITY : uplinkKbps * 1000;
  const active = new Set<LayerId>();
  let total = 0;
  for (const rid of ["q", "h", "f"] as const) {
    const layer = layerOf(rid);
    if (rid === "q" || total + layer.maxBitrate <= budget) {
      active.add(rid);
      total += layer.maxBitrate;
    }
  }
  return SIMULCAST_LADDER.map((l) => ({ ...l, active: active.has(l.rid) }));
}

/** ReceiveContext describes a receiver's situation for layer selection. */
export interface ReceiveContext {
  downlinkKbps: number;
  /** How many remote video tiles are on screen. */
  tileCount: number;
  /** Whether this stream is the focused/active-speaker (large) tile. */
  focused: boolean;
}

/** chooseReceiveLayer picks the layer a receiver should subscribe, or
 *  "audio-only" when the downlink is below the video floor. Full res is reserved
 *  for a focused tile on a good link with few tiles (rtc-lld §3: f = "≤ 4 visible
 *  tiles"). */
export function chooseReceiveLayer(ctx: ReceiveContext): LayerId | "audio-only" {
  if (ctx.downlinkKbps < AUDIO_ONLY_FLOOR_KBPS) return "audio-only";
  if (ctx.focused && ctx.tileCount <= 4 && ctx.downlinkKbps >= 700) return "f";
  if (ctx.downlinkKbps >= 300) return "h";
  return "q";
}

function layerOf(rid: LayerId): SimulcastLayer {
  const l = SIMULCAST_LADDER.find((x) => x.rid === rid);
  if (!l) throw new Error(`unknown layer ${rid}`);
  return l;
}
