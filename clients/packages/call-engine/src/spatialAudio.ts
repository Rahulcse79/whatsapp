// Spatial audio hooks (T9.01): pan each remote participant left↔right by their
// on-screen tile so a group call sounds like the grid looks. Pure math + a
// SpatialPanner port (web: a WebAudio StereoPannerNode per participant; a no-op
// panner is a valid degrade). Runs entirely client-side over the decrypted audio.

/** panForTile maps a tile index (0-based, left→right) to a stereo pan in
 *  [-1, 1]. A single tile is centred; otherwise tiles spread evenly, and the
 *  spread is scaled by `width` (0 = mono/centred, 1 = full stereo). */
export function panForTile(index: number, count: number, width = 0.8): number {
  if (count <= 1 || index < 0 || index >= count) return 0;
  const pos = index / (count - 1); // 0..1 left→right
  return (pos * 2 - 1) * clamp01(width);
}

function clamp01(n: number): number {
  return n < 0 ? 0 : n > 1 ? 1 : n;
}

/** SpatialPanner applies a stereo pan (-1..1) to one participant's decrypted
 *  audio. setPan is idempotent; release tears down the node. */
export interface SpatialPanner {
  setPan(participantId: string, pan: number): void;
  release(participantId: string): void;
}

export class SpatialAudioController {
  private enabled: boolean;
  private order: string[] = []; // current tile order, left→right
  private width: number;

  constructor(
    private readonly panner: SpatialPanner,
    opts: { enabled?: boolean; width?: number } = {},
  ) {
    this.enabled = opts.enabled ?? true;
    this.width = opts.width ?? 0.8;
  }

  isEnabled(): boolean {
    return this.enabled;
  }

  /** setEnabled turns spatialisation on/off; off re-centres everyone (pan 0). */
  setEnabled(on: boolean): void {
    this.enabled = on;
    this.reapply();
  }

  /** layout sets the current left→right participant order (from the grid layout)
   *  and pans each accordingly. Participants who left are released. */
  layout(participantIds: string[]): void {
    for (const id of this.order) {
      if (!participantIds.includes(id)) this.panner.release(id);
    }
    this.order = [...participantIds];
    this.reapply();
  }

  private reapply(): void {
    this.order.forEach((id, i) => {
      this.panner.setPan(id, this.enabled ? panForTile(i, this.order.length, this.width) : 0);
    });
  }
}
