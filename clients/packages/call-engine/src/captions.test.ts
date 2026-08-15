import { describe, expect, it } from "vitest";
import { CaptionController, type SttEngine } from "./captions";

function fakeStt(): SttEngine & { emit: (t: string, f: boolean) => void } {
  let cb: ((t: string, f: boolean) => void) | null = null;
  return {
    start: (onResult) => {
      cb = onResult;
    },
    stop: () => {
      cb = null;
    },
    emit: (t, f) => cb?.(t, f),
  };
}

describe("CaptionController (T9.02)", () => {
  it("streams interim then commits final local captions", () => {
    const stt = fakeStt();
    const c = new CaptionController(stt, "me", undefined, 50, () => 1);
    c.enable();
    stt.emit("hel", false);
    expect(c.livePartial()?.text).toBe("hel");
    expect(c.transcript()).toHaveLength(0); // interim isn't committed
    stt.emit("hello world", true);
    expect(c.livePartial()).toBeNull();
    expect(c.transcript().map((l) => l.text)).toEqual(["hello world"]);
    expect(c.transcript()[0]?.speakerId).toBe("me");
  });

  it("ingests remote peer captions", () => {
    const c = new CaptionController(fakeStt(), "me");
    c.ingest({ id: "p:0", speakerId: "peer", text: "hi from peer", final: true, ts: 1 });
    expect(c.transcript().map((l) => l.speakerId)).toEqual(["peer"]);
  });

  it("caps the rolling transcript and clears partial on disable", () => {
    const stt = fakeStt();
    const c = new CaptionController(stt, "me", undefined, 2, () => 1);
    c.enable();
    stt.emit("a", true);
    stt.emit("b", true);
    stt.emit("c", true);
    expect(c.transcript().map((l) => l.text)).toEqual(["b", "c"]); // maxLines=2
    stt.emit("partial", false);
    expect(c.livePartial()).not.toBeNull();
    c.disable();
    expect(c.livePartial()).toBeNull();
    expect(c.isEnabled()).toBe(false);
  });
});
