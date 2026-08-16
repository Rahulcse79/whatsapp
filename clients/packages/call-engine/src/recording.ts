// Client-side recording with consent signalling (T9.03). Recording is on-device
// only — the SFU forwards ciphertext, so a server-side recording would capture
// nothing decryptable. The host drives session recording state; each client
// records locally *only if it consented*, and every client shows the "recording"
// indicator whenever the session is recording. Pure controller over an injected
// Recorder (MediaRecorder on web / native on mobile); framework-free.

export type SessionRecordingState = "off" | "requested" | "active";

/** Recorder is the platform capture seam: start/stop a local MediaRecorder over
 *  the call's composited stream. stop resolves the captured blob (or null). */
export interface Recorder {
  start(): Promise<void>;
  stop(): Promise<Blob | null>;
}

export interface RecordingState {
  /** the host-driven session state */
  session: SessionRecordingState;
  /** our local consent decision (null = undecided) */
  consented: boolean | null;
  /** are WE capturing locally right now */
  recording: boolean;
  /** should the UI show a "recording" indicator (true whenever the session is active) */
  indicator: boolean;
}

export class RecordingController {
  private session: SessionRecordingState = "off";
  private consented: boolean | null = null;
  private recording = false;
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly recorder: Recorder,
    private readonly onChange?: (s: RecordingState) => void,
    /** notified when the local consent decision changes, so the app can POST it */
    private readonly onDecision?: (consented: boolean) => void,
  ) {}

  getState(): RecordingState {
    return {
      session: this.session,
      consented: this.consented,
      recording: this.recording,
      indicator: this.session === "active",
    };
  }

  /** decide records the local consent answer. A fresh "requested" window resets
   *  the decision (see applyServerState). Starting/stopping capture is reconciled
   *  against the current session state. */
  decide(consented: boolean): Promise<void> {
    this.consented = consented;
    this.onDecision?.(consented);
    return this.reconcile();
  }

  /** applyServerState feeds the polled session recording state in. Entering the
   *  "requested" consent window clears a stale decision so the user is re-asked. */
  applyServerState(next: SessionRecordingState): Promise<void> {
    if (next === "requested" && this.session !== "requested") {
      this.consented = null;
    }
    this.session = next;
    return this.reconcile();
  }

  /** reconcile starts capture iff the session is active AND we consented, and
   *  stops it otherwise. Serialized so overlapping polls can't double-start. */
  private reconcile(): Promise<void> {
    return this.run(async () => {
      const shouldRecord = this.session === "active" && this.consented === true;
      if (shouldRecord && !this.recording) {
        await this.recorder.start();
        this.recording = true;
      } else if (!shouldRecord && this.recording) {
        await this.recorder.stop();
        this.recording = false;
      }
      this.emit();
    });
  }

  private emit(): void {
    this.onChange?.(this.getState());
  }

  private run(op: () => Promise<void>): Promise<void> {
    this.queue = this.queue.then(op, op);
    return this.queue;
  }
}
