import { describe, expect, it } from "vitest";
import { AudioProcessingController, defaultAudioProcessing, type AudioProcessingState } from "./audioProcessing";

function fakeConstrainer() {
  const applied: AudioProcessingState[] = [];
  return {
    applied,
    apply: (s: AudioProcessingState) => {
      applied.push({ ...s });
      return Promise.resolve();
    },
  };
}

describe("AudioProcessingController (T9.01)", () => {
  it("defaults to everything on", () => {
    expect(defaultAudioProcessing).toEqual({ noiseSuppression: true, echoCancellation: true, autoGainControl: true });
  });

  it("toggles a flag and re-applies the whole profile", async () => {
    const c = fakeConstrainer();
    const ctrl = new AudioProcessingController(c);
    await ctrl.setNoiseSuppression(false);
    expect(ctrl.getState().noiseSuppression).toBe(false);
    expect(ctrl.getState().echoCancellation).toBe(true); // unchanged
    expect(c.applied.at(-1)).toEqual({ noiseSuppression: false, echoCancellation: true, autoGainControl: true });
  });

  it("only commits state after the constrainer resolves", async () => {
    let fail = true;
    const ctrl = new AudioProcessingController({
      apply: () => (fail ? Promise.reject(new Error("nope")) : Promise.resolve()),
    });
    await ctrl.setEchoCancellation(false).catch(() => {});
    expect(ctrl.getState().echoCancellation).toBe(true); // rollback: not committed
    fail = false;
    await ctrl.setEchoCancellation(false);
    expect(ctrl.getState().echoCancellation).toBe(false);
  });

  it("toggleNoiseSuppression flips", async () => {
    const ctrl = new AudioProcessingController({ apply: () => Promise.resolve() });
    await ctrl.toggleNoiseSuppression();
    expect(ctrl.getState().noiseSuppression).toBe(false);
    await ctrl.toggleNoiseSuppression();
    expect(ctrl.getState().noiseSuppression).toBe(true);
  });
});
