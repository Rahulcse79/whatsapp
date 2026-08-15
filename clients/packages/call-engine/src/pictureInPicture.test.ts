import { describe, expect, it } from "vitest";
import { PipController, type PipPort } from "./pictureInPicture";

function fakePort(supported = true): PipPort & { fireExit: () => void } {
  let exitCb: (() => void) | null = null;
  return {
    supported: () => supported,
    enter: () => Promise.resolve(),
    exit: () => Promise.resolve(),
    onExit: (cb) => {
      exitCb = cb;
      return () => {
        exitCb = null;
      };
    },
    fireExit: () => exitCb?.(),
  };
}

describe("PipController (T9.01)", () => {
  it("enters and exits, tracking active state", async () => {
    const port = fakePort();
    const ctrl = new PipController(port);
    expect(ctrl.isActive()).toBe(false);
    await ctrl.enter();
    expect(ctrl.isActive()).toBe(true);
    await ctrl.exit();
    expect(ctrl.isActive()).toBe(false);
  });

  it("does nothing when PiP is unsupported", async () => {
    const ctrl = new PipController(fakePort(false));
    await ctrl.enter();
    expect(ctrl.isActive()).toBe(false);
  });

  it("reflects an OS-initiated exit (user closed the window)", async () => {
    const port = fakePort();
    let last = false;
    const ctrl = new PipController(port, (a) => (last = a));
    await ctrl.enter();
    port.fireExit();
    expect(ctrl.isActive()).toBe(false);
    expect(last).toBe(false);
  });

  it("toggle flips", async () => {
    const ctrl = new PipController(fakePort());
    await ctrl.toggle();
    expect(ctrl.isActive()).toBe(true);
    await ctrl.toggle();
    expect(ctrl.isActive()).toBe(false);
  });
});
