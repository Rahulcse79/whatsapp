// Camera management (rtc-lld §3 / T2.05). A small state machine over a platform
// CameraDevice port: enable/disable the local video track and flip front/back,
// serializing device operations so a rapid toggle can't leave the track in a
// half-open state. Framework-free; getUserMedia (web) / react-native-webrtc
// (mobile) implement CameraDevice.

export type CameraFacing = "front" | "back";

export interface CameraState {
  enabled: boolean;
  facing: CameraFacing;
}

/** CameraDevice is the platform capture seam. start opens the track at a facing;
 *  applyFacing switches an already-open track; stop releases it. */
export interface CameraDevice {
  start(facing: CameraFacing): Promise<void>;
  applyFacing(facing: CameraFacing): Promise<void>;
  stop(): Promise<void>;
}

export class CameraController {
  private state: CameraState;
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly device: CameraDevice,
    private readonly onChange?: (s: CameraState) => void,
    initialFacing: CameraFacing = "front",
  ) {
    this.state = { enabled: false, facing: initialFacing };
  }

  getState(): CameraState {
    return this.state;
  }

  /** enable opens the camera (idempotent). */
  enable(): Promise<void> {
    return this.run(async () => {
      if (this.state.enabled) return;
      await this.device.start(this.state.facing);
      this.set({ enabled: true });
    });
  }

  /** disable releases the camera (idempotent). */
  disable(): Promise<void> {
    return this.run(async () => {
      if (!this.state.enabled) return;
      await this.device.stop();
      this.set({ enabled: false });
    });
  }

  /** toggle flips the enabled state. */
  toggle(): Promise<void> {
    return this.state.enabled ? this.disable() : this.enable();
  }

  /** flip switches front/back. If the camera is live the track is re-pointed;
   *  otherwise only the pending facing changes. */
  flip(): Promise<void> {
    return this.run(async () => {
      const facing: CameraFacing = this.state.facing === "front" ? "back" : "front";
      if (this.state.enabled) await this.device.applyFacing(facing);
      this.set({ facing });
    });
  }

  private set(patch: Partial<CameraState>): void {
    this.state = { ...this.state, ...patch };
    this.onChange?.(this.state);
  }

  /** run serializes device operations so overlapping toggles can't race. */
  private run(op: () => Promise<void>): Promise<void> {
    this.queue = this.queue.then(op, op);
    return this.queue;
  }
}
