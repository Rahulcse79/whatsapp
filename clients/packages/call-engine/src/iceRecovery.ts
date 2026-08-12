// ICE-restart recovery (rtc-lld §1: "reconnect < 5 s target"). On a media
// disconnect the controller first tries an ICE restart — renegotiate candidates
// on the same call, cheap and keeps the room; if that doesn't recover quickly it
// escalates to a rejoin (a fresh token via POST /calls/{room}/rejoin, e.g. after
// the SFU node was lost). The whole budget stays under 5 s. Deterministic via an
// injected timer so it is unit-testable.

/** The subset of RTCPeerConnection(ice)ConnectionState we react to. */
export type IceConnState = "connected" | "disconnected" | "failed" | "closed";

export interface IceTransport {
  /** Restart ICE on the existing session (renegotiate candidates). */
  restartIce(): Promise<void>;
}

export interface Rejoiner {
  /** Re-join the room with a fresh token — the last-resort recovery. */
  rejoin(): Promise<void>;
}

export interface Timers {
  /** Schedule fn after ms; returns a cancel function. */
  setTimeout(fn: () => void, ms: number): () => void;
}

export interface IceRecoveryOptions {
  /** Grace after a 'disconnected' blip before restarting (it may self-heal). */
  disconnectGraceMs?: number;
  /** ICE restart attempts before escalating to a rejoin. */
  maxRestarts?: number;
  /** Delay between restart attempts (and before checking for recovery). */
  restartIntervalMs?: number;
}

export class IceRecoveryController {
  private attempts = 0;
  private cancel: (() => void) | null = null;
  private recovering = false;
  private readonly graceMs: number;
  private readonly maxRestarts: number;
  private readonly intervalMs: number;

  constructor(
    private readonly ice: IceTransport,
    private readonly rejoiner: Rejoiner,
    private readonly timers: Timers,
    opts: IceRecoveryOptions = {},
  ) {
    // Defaults keep worst-case recovery under 5 s: grace 1s + 2 restarts × 1.5s.
    this.graceMs = opts.disconnectGraceMs ?? 1000;
    this.maxRestarts = opts.maxRestarts ?? 2;
    this.intervalMs = opts.restartIntervalMs ?? 1500;
  }

  /** onState reacts to the peer connection's ICE / connection state. */
  onState(state: IceConnState): void {
    switch (state) {
      case "connected":
        this.reset(); // recovered, or never lost
        break;
      case "disconnected":
        // A blip: give it a grace window to self-heal, then start restarting.
        if (!this.recovering) this.schedule(this.graceMs, () => this.restartCycle());
        break;
      case "failed":
        // Terminal for this ICE agent: restart immediately.
        this.restartCycle();
        break;
      case "closed":
        this.reset();
        break;
    }
  }

  private restartCycle(): void {
    this.recovering = true;
    if (this.attempts >= this.maxRestarts) {
      // Restarts didn't bring it back — escalate to a fresh-token rejoin.
      void this.rejoiner.rejoin();
      this.reset();
      return;
    }
    this.attempts++;
    void this.ice.restartIce();
    // Re-check after the interval; onState('connected') cancels this.
    this.schedule(this.intervalMs, () => this.restartCycle());
  }

  private schedule(ms: number, fn: () => void): void {
    this.cancel?.();
    this.cancel = this.timers.setTimeout(fn, ms);
  }

  private reset(): void {
    this.cancel?.();
    this.cancel = null;
    this.attempts = 0;
    this.recovering = false;
  }
}
