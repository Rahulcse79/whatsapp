// Active-speaker detection (rtc-lld §3, HLD §10.4). Audio levels ride the RTP
// header extension in the clear, so the client can pick the dominant speaker
// under E2EE without decrypting media. Hysteresis keeps focus on the current
// speaker until it goes quiet for a hold window, so brief overlaps or gaps don't
// thrash the layout / subscription switches.

export interface AudioLevel {
  participantId: string;
  /** Normalized loudness, 0 (silent) .. 1 (loud). */
  level: number;
}

export interface ActiveSpeakerOptions {
  /** Level at or above which a participant counts as speaking. */
  threshold?: number;
  /** How long the current speaker must stay quiet before it yields focus (ms). */
  holdMs?: number;
}

export class ActiveSpeakerTracker {
  private current: string | null = null;
  private quietSince = 0;
  private readonly threshold: number;
  private readonly holdMs: number;

  constructor(opts: ActiveSpeakerOptions = {}) {
    this.threshold = opts.threshold ?? 0.15;
    this.holdMs = opts.holdMs ?? 1500;
  }

  /**
   * update ingests the latest per-participant levels and returns the active
   * speaker (or null when nobody is speaking and the hold has elapsed). Focus
   * only moves to a new speaker once the current one drops below threshold, so a
   * louder-but-brief interjection doesn't steal the tile mid-sentence.
   */
  update(levels: AudioLevel[], now: number): string | null {
    const loudest = levels.filter((l) => l.level >= this.threshold).sort((a, b) => b.level - a.level)[0];

    if (loudest) {
      const cur = this.current ? levels.find((l) => l.participantId === this.current) : undefined;
      // Take focus if there is no current speaker, it has gone quiet, or it has
      // left the call (not present in this report).
      if (loudest.participantId !== this.current && (!cur || cur.level < this.threshold)) {
        this.current = loudest.participantId;
      }
      this.quietSince = 0;
      return this.current;
    }

    // Silence: keep the current speaker until holdMs of continuous quiet.
    if (this.current !== null) {
      if (this.quietSince === 0) this.quietSince = now;
      else if (now - this.quietSince >= this.holdMs) this.current = null;
    }
    return this.current;
  }

  active(): string | null {
    return this.current;
  }
}
