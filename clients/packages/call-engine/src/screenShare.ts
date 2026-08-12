// Screen share (T2.06). A screen share is a SEPARATE track from the camera,
// content-optimized for legibility rather than motion (rtc-lld §3: "detail
// preset, low fps"). This module is the pure control: the content-optimized
// encodings + a share lifecycle state machine over a ScreenSource port. Display
// capture (web: getDisplayMedia; mobile: ReplayKit / MediaProjection) is the
// injected platform adapter. Like every media path, the frames are E2EE-sealed —
// the SFU forwards a stream it cannot read.

export interface ScreenShareState {
  sharing: boolean;
}

/** ScreenSource is the display-capture seam. start opens the OS screen/window
 *  picker and begins capture; stop ends it; onEnded fires when the user stops
 *  sharing via the OS affordance (the "Stop sharing" bar). The adapter publishes
 *  the captured track separately, with the encodings below. */
export interface ScreenSource {
  start(): Promise<void>;
  stop(): Promise<void>;
  onEnded(cb: () => void): void;
}

export interface ScreenEncoding {
  rid: string;
  maxBitrate: number;
  maxFramerate: number;
  scaleResolutionDownBy: number;
}

/** Track content hint applied to a screen-share track — "detail" keeps text
 *  crisp at the cost of framerate (the encoder favours spatial quality). */
export const SCREEN_CONTENT_HINT = "detail";

/** buildScreenShareEncodings returns a content-optimized, low-framerate ladder
 *  (rtc-lld §3): a full-detail rung plus a reduced rung so a large call can still
 *  receive a legible layer on a constrained link. */
export function buildScreenShareEncodings(): ScreenEncoding[] {
  return [
    { rid: "s0", maxBitrate: 2_500_000, maxFramerate: 5, scaleResolutionDownBy: 1 },
    { rid: "s1", maxBitrate: 700_000, maxFramerate: 5, scaleResolutionDownBy: 2 },
  ];
}

export class ScreenShareController {
  private state: ScreenShareState = { sharing: false };
  private queue: Promise<void> = Promise.resolve();
  private wired = false;

  constructor(
    private readonly source: ScreenSource,
    private readonly onChange?: (s: ScreenShareState) => void,
  ) {}

  getState(): ScreenShareState {
    return this.state;
  }

  /** start begins display capture (idempotent). Wires the OS-ended affordance
   *  once so stopping from the system bar reflects here too. */
  start(): Promise<void> {
    return this.run(async () => {
      if (this.state.sharing) return;
      if (!this.wired) {
        this.source.onEnded(() => this.set(false));
        this.wired = true;
      }
      await this.source.start();
      this.set(true);
    });
  }

  /** stop ends display capture (idempotent). */
  stop(): Promise<void> {
    return this.run(async () => {
      if (!this.state.sharing) return;
      await this.source.stop();
      this.set(false);
    });
  }

  toggle(): Promise<void> {
    return this.state.sharing ? this.stop() : this.start();
  }

  private set(sharing: boolean): void {
    this.state = { sharing };
    this.onChange?.(this.state);
  }

  private run(op: () => Promise<void>): Promise<void> {
    this.queue = this.queue.then(op, op);
    return this.queue;
  }
}
