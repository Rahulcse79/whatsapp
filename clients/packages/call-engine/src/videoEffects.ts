// On-device background blur (T2.06). Under E2EE the SFU forwards only encrypted
// frames — there is nothing for a server to process — so background blur MUST run
// on the sender's device, in the capture→encode path, BEFORE the frame is sealed.
// This module is the pure control: an effect state machine over a VideoProcessor
// port. The heavy segmentation (web: MediaPipe/WebGL; mobile: a native frame
// processor) is the injected platform adapter.

// "background" replaces the real background with an image (virtual background,
// T9.01) — same on-device segmentation as blur, compositing over an image
// instead of a blurred copy.
export type BackgroundEffect = "none" | "blur" | "background";

export interface EffectState {
  effect: BackgroundEffect;
  /** The replacement image (data/blob URL) when effect === "background". */
  backgroundImage?: string;
}

/** VideoProcessor inserts/removes an on-device background effect on the LOCAL
 *  video pipeline. It never sends anything anywhere — it only transforms frames
 *  before they are encoded and E2EE-sealed. */
export interface VideoProcessor {
  /** Apply (or switch to) an effect on the local video track. For "background",
   *  opts.backgroundImage is the replacement image. */
  apply(effect: BackgroundEffect, opts?: { backgroundImage?: string }): Promise<void>;
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

  /** setEffect switches the active effect (idempotent). For "background", pass
   *  the replacement image. */
  setEffect(effect: BackgroundEffect, opts?: { backgroundImage?: string }): Promise<void> {
    return this.run(async () => {
      const image = effect === "background" ? opts?.backgroundImage : undefined;
      if (this.state.effect === effect && this.state.backgroundImage === image) return;
      if (effect === "none") await this.processor.clear();
      else await this.processor.apply(effect, { backgroundImage: image });
      this.set(effect, image);
    });
  }

  /** toggleBlur flips between blur and none. */
  toggleBlur(): Promise<void> {
    return this.setEffect(this.state.effect === "blur" ? "none" : "blur");
  }

  /** setBackground applies a virtual-background image (T9.01). */
  setBackground(imageUrl: string): Promise<void> {
    return this.setEffect("background", { backgroundImage: imageUrl });
  }

  private set(effect: BackgroundEffect, backgroundImage?: string): void {
    this.state = backgroundImage ? { effect, backgroundImage } : { effect };
    this.onChange?.(this.state);
  }

  /** run serializes processor operations so a rapid toggle can't leave the
   *  pipeline half-applied. */
  private run(op: () => Promise<void>): Promise<void> {
    this.queue = this.queue.then(op, op);
    return this.queue;
  }
}
