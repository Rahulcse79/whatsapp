// Send-side quality adaptation (rtc-lld §3, HLD §10.5). When the link degrades
// below the video floor (90p @ ~100 kbps) video is dropped and the call goes
// audio-only; video is restored only once the link recovers past a higher
// threshold (hysteresis, so it doesn't flap on a wobbly link). Audio bitrate
// tracks packet loss within the Opus 6–32 kbps band (FEC carries the recovery).
// Pure controller over periodic connection stats.

import { OPUS_MAX_KBPS, OPUS_MIN_KBPS } from "./opusConfig";

/** Below this available send bitrate, video is dropped (rtc-lld §3: 90p floor). */
export const VIDEO_FLOOR_KBPS = 100;
/** The higher bar to bring video back — the hysteresis margin over the floor. */
export const VIDEO_RESTORE_KBPS = 250;
/** Ceiling for the suggested video bitrate (the simulcast full layer, T2.05). */
const VIDEO_CEILING_KBPS = 1200;

export interface ConnectionStats {
  /** Estimated available send bitrate (kbps). */
  availableKbps: number;
  /** Packet loss ratio, 0 (none) .. 1 (total). */
  packetLoss: number;
}

export interface SendQuality {
  video: boolean;
  /** Suggested video bitrate (kbps); 0 when video is off. */
  videoKbps: number;
  /** Suggested audio bitrate (kbps), within the Opus 6–32 band. */
  audioKbps: number;
}

export interface QualityOptions {
  videoFloorKbps?: number;
  videoRestoreKbps?: number;
}

export class QualityController {
  private video = true; // a call starts video-capable and adapts down as needed
  private readonly floor: number;
  private readonly restore: number;

  constructor(
    private readonly onChange?: (q: SendQuality) => void,
    opts: QualityOptions = {},
  ) {
    this.floor = opts.videoFloorKbps ?? VIDEO_FLOOR_KBPS;
    this.restore = opts.videoRestoreKbps ?? VIDEO_RESTORE_KBPS;
  }

  /** videoEnabled reports whether video is currently sent. */
  videoEnabled(): boolean {
    return this.video;
  }

  /**
   * update ingests the latest stats and returns the adapted send quality. Video
   * drops below the floor and is restored only past the (higher) restore
   * threshold.
   */
  update(stats: ConnectionStats): SendQuality {
    if (this.video && stats.availableKbps < this.floor) this.video = false;
    else if (!this.video && stats.availableKbps >= this.restore) this.video = true;

    // Audio: full band at no loss, sliding toward the floor as loss rises (Opus
    // in-band FEC then carries the recovery).
    const penalty = Math.round(stats.packetLoss * (OPUS_MAX_KBPS - OPUS_MIN_KBPS));
    const audioKbps = Math.min(OPUS_MAX_KBPS, Math.max(OPUS_MIN_KBPS, OPUS_MAX_KBPS - penalty));

    const q: SendQuality = this.video
      ? { video: true, videoKbps: clamp(stats.availableKbps - audioKbps, this.floor, VIDEO_CEILING_KBPS), audioKbps }
      : { video: false, videoKbps: 0, audioKbps };
    this.onChange?.(q);
    return q;
  }
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, Math.round(v)));
}
