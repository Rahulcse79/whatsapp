import { describe, expect, it } from "vitest";
import { CameraController, type CameraDevice, type CameraFacing } from "./camera";

class FakeDevice implements CameraDevice {
  log: string[] = [];
  start(facing: CameraFacing): Promise<void> {
    this.log.push(`start:${facing}`);
    return Promise.resolve();
  }
  applyFacing(facing: CameraFacing): Promise<void> {
    this.log.push(`facing:${facing}`);
    return Promise.resolve();
  }
  stop(): Promise<void> {
    this.log.push("stop");
    return Promise.resolve();
  }
}

describe("CameraController", () => {
  it("enable/disable are idempotent and drive the device", async () => {
    const dev = new FakeDevice();
    const cam = new CameraController(dev);
    expect(cam.getState()).toEqual({ enabled: false, facing: "front" });

    await cam.enable();
    await cam.enable(); // idempotent
    expect(cam.getState().enabled).toBe(true);

    await cam.disable();
    await cam.disable(); // idempotent
    expect(cam.getState().enabled).toBe(false);
    expect(dev.log).toEqual(["start:front", "stop"]);
  });

  it("flip switches a live track and only records facing while off", async () => {
    const dev = new FakeDevice();
    const cam = new CameraController(dev);

    await cam.flip(); // camera off → just records the facing change
    expect(cam.getState().facing).toBe("back");
    expect(dev.log).toEqual([]);

    await cam.enable();
    await cam.flip(); // live → re-points the track
    expect(cam.getState().facing).toBe("front");
    expect(dev.log).toEqual(["start:back", "facing:front"]);
  });

  it("toggle flips enabled state", async () => {
    const cam = new CameraController(new FakeDevice());
    await cam.toggle();
    expect(cam.getState().enabled).toBe(true);
    await cam.toggle();
    expect(cam.getState().enabled).toBe(false);
  });

  it("serializes overlapping operations (no half-open races)", async () => {
    const dev = new FakeDevice();
    const cam = new CameraController(dev);
    // Fire enable + disable + enable without awaiting between them.
    await Promise.all([cam.enable(), cam.disable(), cam.enable()]);
    expect(cam.getState().enabled).toBe(true);
    // Operations ran in order, not interleaved.
    expect(dev.log).toEqual(["start:front", "stop", "start:front"]);
  });

  it("emits state changes to the listener", async () => {
    const states: boolean[] = [];
    const cam = new CameraController(new FakeDevice(), (s) => states.push(s.enabled));
    await cam.enable();
    await cam.disable();
    expect(states).toEqual([true, false]);
  });
});
