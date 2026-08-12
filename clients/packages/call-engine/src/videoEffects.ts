// On-device background blur (T2.06). Under E2EE the SFU forwards only encrypted
// frames — there is nothing for a server to process — so background blur MUST run
// on the sender's device, in the capture→encode path, BEFORE the frame is sealed.
// This module is the pure control: an effect state machine over a VideoProcessor
// port. The heavy segmentation (web: MediaPipe/WebGL; mobile: a native frame
// processor) is the injected platform adapter.

export type BackgroundEffect = "none" | "blur";

export interface EffectState {
  effect: BackgroundEffect;
}

/** VideoProcessor inserts/removes an on-device background effect on the LOCAL
 *  video pipeline. It never sends anything anywhere — it only transforms frames
 *  before they are encoded and E2EE-sealed. */
export interface VideoProcessor {
  /** Apply (or switch to) an effect on the local video track. */
  apply(effect: BackgroundEffect): Promise<void>;
  /** Remove the effect, restoring the raw camera feed. */
  clear(): Promise<void>;
}

export class EffectController {
  private state: EffectState = { effect: "none" };
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly processor: VideoProcessor,
    private readonly onChange?: (s: EffectState) => void,
  ) {}

  getState(): EffectState {
    return this.state;
  }

  /** setEffect switches the active effect (idempotent). */
  setEffect(effect: BackgroundEffect): Promise<void> {
    return this.run(async () => {
      if (this.state.effect === effect) return;
      if (effect === "none") await this.processor.clear();
      else await this.processor.apply(effect);
      this.set(effect);
    });
  }

  /** toggleBlur flips between blur and none. */
  toggleBlur(): Promise<void> {
    return this.setEffect(this.state.effect === "blur" ? "none" : "blur");
  }

  private set(effect: BackgroundEffect): void {
    this.state = { effect };
    this.onChange?.(this.state);
  }

  /** run serializes processor operations so a rapid toggle can't leave the
   *  pipeline half-applied. */
  private run(op: () => Promise<void>): Promise<void> {
    this.queue = this.queue.then(op, op);
    return this.queue;
  }
}
