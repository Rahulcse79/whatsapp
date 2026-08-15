// On-device audio processing (T9.01): noise suppression, echo cancellation, and
// auto-gain. These are WebRTC getUserMedia/track constraints applied to the LOCAL
// mic track before capture — client-side, so nothing about the raw audio leaves
// the device beyond the E2EE call. Pure control here; the platform injects an
// AudioConstrainer that calls MediaStreamTrack.applyConstraints (web) or the RN
// equivalent.

export interface AudioProcessingState {
  noiseSuppression: boolean;
  echoCancellation: boolean;
  autoGainControl: boolean;
}

/** The default processing profile — everything on, matching what a caller
 *  expects on a voice/video call. */
export const defaultAudioProcessing: AudioProcessingState = {
  noiseSuppression: true,
  echoCancellation: true,
  autoGainControl: true,
};

/** AudioConstrainer applies the processing flags to the live mic track. */
export interface AudioConstrainer {
  apply(state: AudioProcessingState): Promise<void>;
}

export class AudioProcessingController {
  private state: AudioProcessingState;
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly constrainer: AudioConstrainer,
    initial: AudioProcessingState = defaultAudioProcessing,
    private readonly onChange?: (s: AudioProcessingState) => void,
  ) {
    this.state = { ...initial };
  }

  getState(): AudioProcessingState {
    return this.state;
  }

  setNoiseSuppression(on: boolean): Promise<void> {
    return this.update({ noiseSuppression: on });
  }
  setEchoCancellation(on: boolean): Promise<void> {
    return this.update({ echoCancellation: on });
  }
  setAutoGainControl(on: boolean): Promise<void> {
    return this.update({ autoGainControl: on });
  }
  toggleNoiseSuppression(): Promise<void> {
    return this.setNoiseSuppression(!this.state.noiseSuppression);
  }

  /** update merges a partial change, re-applies the whole profile, and (only on
   *  success) commits + emits. Operations serialize so rapid toggles can't race
   *  the track. */
  private update(patch: Partial<AudioProcessingState>): Promise<void> {
    const next = { ...this.state, ...patch };
    this.queue = this.queue.then(
      async () => {
        await this.constrainer.apply(next);
        this.state = next;
        this.onChange?.(this.state);
      },
      async () => {
        await this.constrainer.apply(next);
        this.state = next;
        this.onChange?.(this.state);
      },
    );
    return this.queue;
  }
}
