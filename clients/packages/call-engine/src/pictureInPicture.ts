// Picture-in-Picture (T9.01): float the call's remote video in an OS-level PiP
// window so the call stays visible while the user does something else. Pure
// control over a PipPort — the platform injects the real implementation (web:
// HTMLVideoElement.requestPictureInPicture; mobile: native PiP). The port also
// reports OS-initiated exits (the user closing the PiP window) so state stays
// truthful.

export interface PipPort {
  supported(): boolean;
  enter(): Promise<void>;
  exit(): Promise<void>;
  /** Register a callback the port fires when PiP ends outside our control
   *  (user closed the floating window, track ended, …). Returns an unsubscribe. */
  onExit(cb: () => void): () => void;
}

export class PipController {
  private active = false;
  private unbind: (() => void) | null = null;

  constructor(
    private readonly port: PipPort,
    private readonly onChange?: (active: boolean) => void,
  ) {}

  supported(): boolean {
    return this.port.supported();
  }
  isActive(): boolean {
    return this.active;
  }

  async enter(): Promise<void> {
    if (this.active || !this.port.supported()) return;
    await this.port.enter();
    this.active = true;
    // Track OS-initiated exits so isActive() can't get stuck true.
    this.unbind = this.port.onExit(() => this.markExited());
    this.onChange?.(true);
  }

  async exit(): Promise<void> {
    if (!this.active) return;
    await this.port.exit();
    this.markExited();
  }

  toggle(): Promise<void> {
    return this.active ? this.exit() : this.enter();
  }

  private markExited(): void {
    if (!this.active) return;
    this.active = false;
    this.unbind?.();
    this.unbind = null;
    this.onChange?.(false);
  }
}
