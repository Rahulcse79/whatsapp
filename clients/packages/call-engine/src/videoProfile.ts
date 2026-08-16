// Video profiles incl. a 4K tier (T9.03). Pure resolution/bitrate ladder plus a
// small controller that applies a chosen profile to the sender over an injected
// VideoConstrainer (RTCRtpSender.setParameters / track constraints). The 4K tier
// is opt-in and only chosen when the connection budget allows. Framework-free.

export type VideoProfileId = "360p" | "720p" | "1080p" | "4k";

export interface VideoProfile {
  id: VideoProfileId;
  width: number;
  height: number;
  fps: number;
  maxKbps: number;
}

// Ordered low → high. maxKbps are sensible VP9/H.264 ceilings per tier.
export const VIDEO_PROFILES: Record<VideoProfileId, VideoProfile> = {
  "360p": { id: "360p", width: 640, height: 360, fps: 30, maxKbps: 600 },
  "720p": { id: "720p", width: 1280, height: 720, fps: 30, maxKbps: 1700 },
  "1080p": { id: "1080p", width: 1920, height: 1080, fps: 30, maxKbps: 3500 },
  "4k": { id: "4k", width: 3840, height: 2160, fps: 30, maxKbps: 12000 },
};

const LADDER: VideoProfileId[] = ["360p", "720p", "1080p", "4k"];

/** profileForBudget picks the highest profile whose ceiling fits the available
 *  uplink (kbps). The 4K tier is gated behind `allow4k` (opt-in — most callers
 *  cap at 1080p). Always returns at least 360p. */
export function profileForBudget(availableKbps: number, allow4k = false): VideoProfile {
  let chosen: VideoProfileId = "360p";
  for (const id of LADDER) {
    if (id === "4k" && !allow4k) break;
    if (VIDEO_PROFILES[id].maxKbps <= availableKbps) chosen = id;
  }
  return VIDEO_PROFILES[chosen];
}

/** VideoConstrainer applies a profile to the local video sender/track. */
export interface VideoConstrainer {
  apply(profile: VideoProfile): Promise<void>;
}

export interface VideoProfileState {
  profile: VideoProfile;
  allow4k: boolean;
}

export class VideoProfileController {
  private state: VideoProfileState;
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly constrainer: VideoConstrainer,
    initial: VideoProfileId = "720p",
    allow4k = false,
    private readonly onChange?: (s: VideoProfileState) => void,
  ) {
    this.state = { profile: VIDEO_PROFILES[initial], allow4k };
  }

  getState(): VideoProfileState {
    return this.state;
  }

  /** allow4k opts the sender into (or out of) the 4K tier. */
  setAllow4k(allow: boolean): void {
    this.state = { ...this.state, allow4k: allow };
    this.onChange?.(this.state);
  }

  /** set applies a specific profile. 4K is rejected unless opted in. */
  set(id: VideoProfileId): Promise<void> {
    if (id === "4k" && !this.state.allow4k) {
      return Promise.reject(new Error("videoProfile: enable allow4k before selecting 4K"));
    }
    return this.applyProfile(VIDEO_PROFILES[id]);
  }

  /** adapt picks + applies the best profile for the current uplink budget. */
  adapt(availableKbps: number): Promise<void> {
    return this.applyProfile(profileForBudget(availableKbps, this.state.allow4k));
  }

  private applyProfile(profile: VideoProfile): Promise<void> {
    return this.run(async () => {
      if (this.state.profile.id === profile.id) return;
      await this.constrainer.apply(profile);
      this.state = { ...this.state, profile };
      this.onChange?.(this.state);
    });
  }

  private run(op: () => Promise<void>): Promise<void> {
    this.queue = this.queue.then(op, op);
    return this.queue;
  }
}
