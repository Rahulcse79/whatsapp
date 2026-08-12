import { describe, expect, it } from "vitest";
import { IceRecoveryController, type IceTransport, type Rejoiner, type Timers } from "./iceRecovery";

class FakeTimers implements Timers {
  private queue: Array<{ fn: () => void }> = [];
  setTimeout(fn: () => void): () => void {
    const entry = { fn };
    this.queue.push(entry);
    return () => {
      const i = this.queue.indexOf(entry);
      if (i >= 0) this.queue.splice(i, 1);
    };
  }
  /** Fire the next scheduled callback (advances the recovery cycle). */
  fireNext(): void {
    this.queue.shift()?.fn();
  }
  pending(): number {
    return this.queue.length;
  }
}

function setup(opts = { maxRestarts: 2 }) {
  const timers = new FakeTimers();
  const ice = { calls: 0, restartIce() { this.calls++; return Promise.resolve(); } } satisfies IceTransport & { calls: number };
  const rejoiner = { calls: 0, rejoin() { this.calls++; return Promise.resolve(); } } satisfies Rejoiner & { calls: number };
  const c = new IceRecoveryController(ice, rejoiner, timers, opts);
  return { c, timers, ice, rejoiner };
}

describe("IceRecoveryController", () => {
  it("restarts ICE on failure, escalating to rejoin after maxRestarts", () => {
    const { c, timers, ice, rejoiner } = setup();
    c.onState("failed");
    expect(ice.calls).toBe(1); // attempt 1 immediately
    timers.fireNext();
    expect(ice.calls).toBe(2); // attempt 2 after the interval
    timers.fireNext();
    expect(rejoiner.calls).toBe(1); // attempts exhausted → fresh-token rejoin
  });

  it("stops recovering once ICE reconnects (no rejoin)", () => {
    const { c, timers, ice, rejoiner } = setup();
    c.onState("failed");
    expect(ice.calls).toBe(1);
    c.onState("connected"); // recovered → pending check cancelled
    expect(timers.pending()).toBe(0);
    timers.fireNext(); // no-op
    expect(rejoiner.calls).toBe(0);
  });

  it("gives a 'disconnected' blip a grace window before restarting", () => {
    const { c, timers, ice } = setup();
    c.onState("disconnected");
    expect(ice.calls).toBe(0); // grace first — the blip may self-heal
    timers.fireNext();
    expect(ice.calls).toBe(1); // grace elapsed → restart
  });
});
