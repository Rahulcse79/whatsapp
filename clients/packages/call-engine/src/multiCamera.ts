// Multi-camera (T9.03): enumerate the device's cameras, pick a primary, and
// optionally publish a *second* camera simultaneously (e.g. front + back, or a
// document camera). The primary track is owned by CameraController; this manages
// the enumeration + the secondary publish over injected ports. Framework-free.

export interface CameraInfo {
  deviceId: string;
  label: string;
}

/** CameraEnumerator lists capture devices (navigator.mediaDevices on web). */
export interface CameraEnumerator {
  list(): Promise<CameraInfo[]>;
}

/** SecondaryCamera publishes/unpublishes an extra camera track alongside the
 *  primary one (a second LiveKit track). */
export interface SecondaryCamera {
  publish(deviceId: string): Promise<void>;
  unpublish(): Promise<void>;
}

export interface MultiCameraState {
  cameras: CameraInfo[];
  primaryId: string | null;
  secondaryId: string | null;
}

export class MultiCameraController {
  private state: MultiCameraState = { cameras: [], primaryId: null, secondaryId: null };
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly enumerator: CameraEnumerator,
    private readonly secondary: SecondaryCamera,
    private readonly onChange?: (s: MultiCameraState) => void,
  ) {}

  getState(): MultiCameraState {
    return this.state;
  }

  /** refresh re-enumerates cameras. Defaults the primary to the first camera if
   *  none is chosen yet. */
  refresh(): Promise<CameraInfo[]> {
    return this.enqueue(async () => {
      const cameras = await this.enumerator.list();
      const primaryId = this.state.primaryId ?? cameras[0]?.deviceId ?? null;
      this.set({ cameras, primaryId });
      return cameras;
    });
  }

  /** selectPrimary marks the main camera (the CameraController does the actual
   *  device switch). If it matches the secondary, the secondary is dropped. */
  selectPrimary(deviceId: string): Promise<void> {
    return this.enqueue(async () => {
      if (this.state.secondaryId === deviceId) {
        await this.secondary.unpublish();
        this.set({ primaryId: deviceId, secondaryId: null });
      } else {
        this.set({ primaryId: deviceId });
      }
    });
  }

  /** enableSecondary publishes a second camera. It must differ from the primary. */
  enableSecondary(deviceId: string): Promise<void> {
    return this.enqueue(async () => {
      if (deviceId === this.state.primaryId) {
        throw new Error("multiCamera: secondary must differ from the primary camera");
      }
      await this.secondary.publish(deviceId);
      this.set({ secondaryId: deviceId });
    });
  }

  /** disableSecondary stops the second camera (idempotent). */
  disableSecondary(): Promise<void> {
    return this.enqueue(async () => {
      if (this.state.secondaryId === null) return;
      await this.secondary.unpublish();
      this.set({ secondaryId: null });
    });
  }

  private set(patch: Partial<MultiCameraState>): void {
    this.state = { ...this.state, ...patch };
    this.onChange?.(this.state);
  }

  private enqueue<T>(op: () => Promise<T>): Promise<T> {
    const run = this.queue.then(op, op);
    this.queue = run.then(
      () => undefined,
      () => undefined,
    );
    return run;
  }
}
