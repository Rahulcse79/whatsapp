import { describe, expect, it } from "vitest";
import { RecordingController, type Recorder } from "./recording";

class FakeRecorder implements Recorder {
  log: string[] = [];
  start(): Promise<void> {
    this.log.push("start");
    return Promise.resolve();
  }
  stop(): Promise<Blob | null> {
    this.log.push("stop");
    return Promise.resolve(null);
  }
}

describe("RecordingController", () => {
  it("records locally only after consent + active, and shows the indicator for all", async () => {
    const rec = new FakeRecorder();
    const c = new RecordingController(rec);

    await c.applyServerState("requested"); // consent window opens
    expect(c.getState().consented).toBeNull();
    expect(c.getState().indicator).toBe(false);

    await c.decide(true);
    await c.applyServerState("active");
    expect(rec.log).toEqual(["start"]);
    expect(c.getState()).toMatchObject({ recording: true, indicator: true });

    await c.applyServerState("off");
    expect(rec.log).toEqual(["start", "stop"]);
    expect(c.getState().recording).toBe(false);
  });

  it("does not record when the user declines, but still shows the indicator", async () => {
    const rec = new FakeRecorder();
    const c = new RecordingController(rec);
    await c.applyServerState("requested");
    await c.decide(false);
    await c.applyServerState("active");
    expect(rec.log).toEqual([]); // never captured locally
    expect(c.getState().indicator).toBe(true); // still notified recording is on
  });

  it("re-asks for consent on a fresh request window", async () => {
    const c = new RecordingController(new FakeRecorder());
    await c.decide(true);
    await c.applyServerState("requested");
    expect(c.getState().consented).toBeNull();
  });

  it("reports the local decision via onDecision", async () => {
    const decisions: boolean[] = [];
    const c = new RecordingController(new FakeRecorder(), undefined, (d) => decisions.push(d));
    await c.decide(true);
    expect(decisions).toEqual([true]);
  });
});
