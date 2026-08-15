// On-device live captions (T9.02). Speech-to-text runs ENTIRELY on the speaker's
// device (web: Web Speech API / an on-device model; mobile: native STT) — the
// raw audio never leaves the device beyond the E2EE call, and the resulting
// caption text is shared with peers over the (E2EE) data channel by the app.
// This is the pure control: it drives the local STT engine, keeps a rolling
// transcript, and ingests peers' caption lines. The SttEngine is injected.

export interface CaptionLine {
  id: string;
  speakerId: string;
  text: string;
  final: boolean; // false = live/interim; true = committed
  ts: number;
}

/** SttEngine streams interim then final transcripts for the LOCAL mic. */
export interface SttEngine {
  start(onResult: (text: string, final: boolean) => void): void;
  stop(): void;
}

export class CaptionController {
  private enabled = false;
  private lines: CaptionLine[] = []; // rolling final transcript
  private partial: CaptionLine | null = null; // the live local line, if any
  private seq = 0;

  constructor(
    private readonly stt: SttEngine,
    private readonly selfId: string,
    private readonly onChange?: (lines: CaptionLine[], partial: CaptionLine | null) => void,
    private readonly maxLines = 50,
    private readonly now: () => number = () => Date.now(),
  ) {}

  isEnabled(): boolean {
    return this.enabled;
  }

  /** enable starts on-device STT for the local speaker. */
  enable(): void {
    if (this.enabled) return;
    this.enabled = true;
    this.stt.start((text, final) => this.onLocal(text, final));
  }

  disable(): void {
    if (!this.enabled) return;
    this.enabled = false;
    this.stt.stop();
    this.partial = null;
    this.emit();
  }

  /** ingest folds in a caption line received from a peer over the data channel. */
  ingest(line: CaptionLine): void {
    this.append(line);
    this.emit();
  }

  transcript(): CaptionLine[] {
    return [...this.lines];
  }
  livePartial(): CaptionLine | null {
    return this.partial;
  }

  private onLocal(text: string, final: boolean): void {
    if (final) {
      this.append({ id: `${this.selfId}:${this.seq++}`, speakerId: this.selfId, text, final: true, ts: this.now() });
      this.partial = null;
    } else {
      this.partial = { id: `${this.selfId}:partial`, speakerId: this.selfId, text, final: false, ts: this.now() };
    }
    this.emit();
  }

  private append(line: CaptionLine): void {
    this.lines.push(line);
    if (this.lines.length > this.maxLines) this.lines.shift();
  }

  private emit(): void {
    this.onChange?.([...this.lines], this.partial);
  }
}
