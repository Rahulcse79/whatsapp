import { describe, expect, it } from "vitest";
import type { CallState } from "./callState";
import { RingBridge, type CallActions, type IncomingCall, type NativeRinger } from "./ringBridge";

class FakeRinger implements NativeRinger {
  events: string[] = [];
  private answerCb: ((r: string) => void) | null = null;
  private endCb: ((r: string) => void) | null = null;
  reportIncoming(c: IncomingCall): void {
    this.events.push(`incoming:${c.ringId}`);
  }
  reportOutgoing(c: IncomingCall): void {
    this.events.push(`outgoing:${c.ringId}`);
  }
  reportConnected(r: string): void {
    this.events.push(`connected:${r}`);
  }
  reportEnded(r: string, reason: string): void {
    this.events.push(`ended:${r}:${reason}`);
  }
  onAnswer(cb: (r: string) => void): void {
    this.answerCb = cb;
  }
  onEnd(cb: (r: string) => void): void {
    this.endCb = cb;
  }
  fireAnswer(): void {
    this.answerCb?.("r1");
  }
  fireEnd(): void {
    this.endCb?.("r1");
  }
}

class FakeActions implements CallActions {
  calls: string[] = [];
  accept(): Promise<void> {
    this.calls.push("accept");
    return Promise.resolve();
  }
  decline(): Promise<void> {
    this.calls.push("decline");
    return Promise.resolve();
  }
  hangup(): Promise<void> {
    this.calls.push("hangup");
    return Promise.resolve();
  }
  onOffer(peerId: string, _roomId: string, ringId: string): void {
    this.calls.push(`offer:${peerId}:${ringId}`);
  }
}

const incoming: IncomingCall = { ringId: "r1", roomId: "call-1", peerId: "bob", kind: "voice" };

describe("RingBridge", () => {
  it("reports to CallKit BEFORE surfacing the offer (iOS immediacy)", () => {
    const ringer = new FakeRinger();
    const actions = new FakeActions();
    new RingBridge(ringer, actions).incoming(incoming);
    expect(ringer.events).toEqual(["incoming:r1"]);
    expect(actions.calls).toEqual(["offer:bob:r1"]);
  });

  it("answer from the lock screen accepts the call", () => {
    const ringer = new FakeRinger();
    const actions = new FakeActions();
    new RingBridge(ringer, actions).incoming(incoming);
    ringer.fireAnswer();
    expect(actions.calls).toContain("accept");
  });

  it("ending a ringing call declines; ending an active call hangs up", () => {
    const ringer = new FakeRinger();
    const actions = new FakeActions();
    const bridge = new RingBridge(ringer, actions);
    bridge.incoming(incoming);
    ringer.fireEnd(); // still ringing → decline
    expect(actions.calls).toContain("decline");

    // Now connected, then end again → hangup.
    bridge.onState({ phase: "connected", ringId: "r1" } as CallState);
    ringer.fireEnd();
    expect(actions.calls).toContain("hangup");
  });

  it("mirrors session state onto the native call UI", () => {
    const ringer = new FakeRinger();
    const bridge = new RingBridge(ringer, new FakeActions());

    bridge.onState({ phase: "outgoing", ringId: "r1", roomId: "call-1", peerId: "bob", kind: "voice" });
    bridge.onState({ phase: "connecting", ringId: "r1" });
    bridge.onState({ phase: "connected", ringId: "r1" });
    bridge.onState({ phase: "ended", ringId: "r1", endReason: "hangup" });

    expect(ringer.events).toEqual(["outgoing:r1", "connected:r1", "ended:r1:hangup"]);
  });

  it("fires each native transition once (idempotent across repeats)", () => {
    const ringer = new FakeRinger();
    const bridge = new RingBridge(ringer, new FakeActions());
    bridge.onState({ phase: "connected", ringId: "r1" });
    bridge.onState({ phase: "connected", ringId: "r1" }); // repeat → no duplicate
    expect(ringer.events.filter((e) => e.startsWith("connected"))).toHaveLength(1);
  });
});
