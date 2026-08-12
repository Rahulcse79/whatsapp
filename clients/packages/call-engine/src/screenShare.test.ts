import { describe, expect, it } from "vitest";
import { buildScreenShareEncodings, ScreenShareController, type ScreenSource } from "./screenShare";

class FakeSource implements ScreenSource {
  log: string[] = [];
  private ended: (() => void) | null = null;
  start(): Promise<void> {
    this.log.push("start");
    return Promise.resolve();
  }
  stop(): Promise<void> {
    this.log.push("stop");
    return Promise.resolve();
  }
  onEnded(cb: () => void): void {
    this.ended = cb;
  }
  /** Simulate the OS "Stop sharing" affordance. */
  osStop(): void {
    this.ended?.();
  }
}

describe("buildScreenShareEncodings", () => {
  it("is content-optimized: low framerate, a full + reduced rung", () => {
    const enc = buildScreenShareEncodings();
    expect(enc.map((e) => e.rid)).toEqual(["s0", "s1"]);
    expect(enc.every((e) => e.maxFramerate <= 5)).toBe(true); // detail preset, low fps
    expect(enc[0]?.scaleResolutionDownBy).toBe(1); // s0 is full resolution
  });
});

describe("ScreenShareController", () => {
  it("start/stop drive the source and are idempotent", async () => {
    const src = new FakeSource();
    const c = new ScreenShareController(src);
    expect(c.getState().sharing).toBe(false);

    await c.start();
    await c.start(); // idempotent
    expect(c.getState().sharing).toBe(true);

    await c.stop();
    await c.stop(); // idempotent
    expect(c.getState().sharing).toBe(false);
    expect(src.log).toEqual(["start", "stop"]);
  });

  it("reflects the OS 'Stop sharing' affordance", async () => {
    const seen: boolean[] = [];
    const src = new FakeSource();
    const c = new ScreenShareController(src, (s) => seen.push(s.sharing));
    await c.start();
    src.osStop(); // user stopped from the system bar
    expect(c.getState().sharing).toBe(false);
    expect(seen).toEqual([true, false]);
  });

  it("toggle flips sharing state", async () => {
    const c = new ScreenShareController(new FakeSource());
    await c.toggle();
    expect(c.getState().sharing).toBe(true);
    await c.toggle();
    expect(c.getState().sharing).toBe(false);
  });
});
