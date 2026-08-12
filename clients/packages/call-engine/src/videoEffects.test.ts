import { describe, expect, it } from "vitest";
import { EffectController, type BackgroundEffect, type VideoProcessor } from "./videoEffects";

class FakeProcessor implements VideoProcessor {
  log: string[] = [];
  apply(effect: BackgroundEffect): Promise<void> {
    this.log.push(`apply:${effect}`);
    return Promise.resolve();
  }
  clear(): Promise<void> {
    this.log.push("clear");
    return Promise.resolve();
  }
}

describe("EffectController", () => {
  it("applies blur, clears back to none, and is idempotent", async () => {
    const p = new FakeProcessor();
    const c = new EffectController(p);
    expect(c.getState().effect).toBe("none");

    await c.setEffect("blur");
    await c.setEffect("blur"); // idempotent
    expect(c.getState().effect).toBe("blur");

    await c.setEffect("none");
    await c.setEffect("none"); // idempotent
    expect(c.getState().effect).toBe("none");
    expect(p.log).toEqual(["apply:blur", "clear"]);
  });

  it("toggleBlur flips between blur and none", async () => {
    const c = new EffectController(new FakeProcessor());
    await c.toggleBlur();
    expect(c.getState().effect).toBe("blur");
    await c.toggleBlur();
    expect(c.getState().effect).toBe("none");
  });

  it("serializes overlapping toggles (no half-applied pipeline)", async () => {
    const p = new FakeProcessor();
    const c = new EffectController(p);
    await Promise.all([c.setEffect("blur"), c.setEffect("none"), c.setEffect("blur")]);
    expect(c.getState().effect).toBe("blur");
    expect(p.log).toEqual(["apply:blur", "clear", "apply:blur"]);
  });

  it("emits state changes", async () => {
    const seen: BackgroundEffect[] = [];
    const c = new EffectController(new FakeProcessor(), (s) => seen.push(s.effect));
    await c.toggleBlur();
    await c.toggleBlur();
    expect(seen).toEqual(["blur", "none"]);
  });
});
