import { describe, expect, it } from "vitest";
import { active, IDLE, reduce, type CallState } from "./callState";

const outgoing = (): CallState =>
  reduce(IDLE, { t: "start", peerId: "bob", roomId: "call-1", ringId: "r1", kind: "voice" });
const incoming = (): CallState =>
  reduce(IDLE, { t: "offer", peerId: "alice", roomId: "call-1", ringId: "r1", kind: "voice" });

describe("reduce", () => {
  it("caller: start → outgoing → answered → connecting → connected", () => {
    let s = outgoing();
    expect(s.phase).toBe("outgoing");
    expect(s.direction).toBe("outgoing");
    s = reduce(s, { t: "ring", state: "answered" });
    expect(s.phase).toBe("connecting");
    s = reduce(s, { t: "media-connected" });
    expect(s.phase).toBe("connected");
  });

  it("callee: offer → incoming → accept → connecting → connected", () => {
    let s = incoming();
    expect(s.phase).toBe("incoming");
    s = reduce(s, { t: "accept" });
    expect(s.phase).toBe("connecting");
    s = reduce(s, { t: "media-connected" });
    expect(s.phase).toBe("connected");
  });

  it("caller ring outcomes end the call with a reason", () => {
    for (const [signal, reason] of [
      ["declined", "declined"],
      ["busy", "busy"],
      ["missed", "missed"],
    ] as const) {
      const s = reduce(outgoing(), { t: "ring", state: signal });
      expect(s.phase).toBe("ended");
      expect(s.endReason).toBe(reason);
    }
  });

  it("callee: answered_elsewhere ends this device's ring", () => {
    const s = reduce(incoming(), { t: "ring", state: "answered_elsewhere" });
    expect(s.phase).toBe("ended");
    expect(s.endReason).toBe("answered_elsewhere");
  });

  it("hangup and remote-end and media-failed are terminal with reasons", () => {
    expect(reduce(outgoing(), { t: "hangup" })).toMatchObject({ phase: "ended", endReason: "hangup" });
    expect(reduce(incoming(), { t: "remote-end", reason: "peer_gone" })).toMatchObject({ phase: "ended", endReason: "peer_gone" });
    const connecting = reduce(incoming(), { t: "accept" });
    expect(reduce(connecting, { t: "media-failed" })).toMatchObject({ phase: "ended", endReason: "media_failed" });
  });

  it("ignores an offer while already in a call, and freezes terminal state", () => {
    const busy = reduce(outgoing(), { t: "offer", peerId: "x", roomId: "y", ringId: "z", kind: "voice" });
    expect(busy.phase).toBe("outgoing"); // offer ignored

    const ended = reduce(outgoing(), { t: "hangup" });
    expect(reduce(ended, { t: "ring", state: "answered" })).toBe(ended); // frozen (same ref)
  });

  it("active() is true only for in-progress phases", () => {
    expect(active(IDLE)).toBe(false);
    expect(active(outgoing())).toBe(true);
    expect(active(reduce(outgoing(), { t: "hangup" }))).toBe(false);
  });
});
