import { describe, expect, it } from "vitest";
import { CallSession, type CallControl, type CreateResult, type MediaConnectOptions, type MediaTransport, type RootSecretProvider } from "./callSession";

class FakeControl implements CallControl {
  created: Array<{ peerId: string; kind: string }> = [];
  answered: string[] = [];
  declined: string[] = [];
  create(peerId: string, kind: "voice" | "video"): Promise<CreateResult> {
    this.created.push({ peerId, kind });
    return Promise.resolve({ roomId: "call-1", ringId: "r1", joinToken: "caller-token" });
  }
  answer(ringId: string): Promise<string> {
    this.answered.push(ringId);
    return Promise.resolve("callee-token");
  }
  decline(ringId: string): Promise<void> {
    this.declined.push(ringId);
    return Promise.resolve();
  }
  rejoin(): Promise<string> {
    return Promise.resolve("rejoin-token");
  }
}

class FakeMedia implements MediaTransport {
  connects: MediaConnectOptions[] = [];
  disconnects = 0;
  fail = false;
  connect(opts: MediaConnectOptions): Promise<void> {
    this.connects.push(opts);
    return this.fail ? Promise.reject(new Error("ice failed")) : Promise.resolve();
  }
  disconnect(): Promise<void> {
    this.disconnects++;
    return Promise.resolve();
  }
}

const secrets: RootSecretProvider = { callSecret: () => Promise.resolve(new Uint8Array(32).fill(7)) };

function setup(fail = false) {
  const control = new FakeControl();
  const media = new FakeMedia();
  media.fail = fail;
  const states: string[] = [];
  const session = new CallSession(control, media, secrets, "self", (s) => states.push(s.phase));
  return { control, media, session, states };
}

describe("CallSession", () => {
  it("caller: create → ringing → answered → media connects → connected", async () => {
    const { control, media, session } = setup();
    await session.startCall("bob", "voice");
    expect(control.created).toEqual([{ peerId: "bob", kind: "voice" }]);
    expect(session.getState().phase).toBe("outgoing");

    await session.onRing("answered");
    expect(media.connects).toHaveLength(1);
    expect(media.connects[0]!.joinToken).toBe("caller-token");
    expect(media.connects[0]!.roomId).toBe("call-1");
    expect(session.getState().phase).toBe("connected");
  });

  it("callee: offer → accept → answer token → media connects → connected", async () => {
    const { control, media, session } = setup();
    session.onOffer("alice", "call-1", "r1", "voice");
    expect(session.getState().phase).toBe("incoming");

    await session.accept();
    expect(control.answered).toEqual(["r1"]);
    expect(media.connects[0]!.joinToken).toBe("callee-token");
    expect(session.getState().phase).toBe("connected");
  });

  it("callee decline calls control.decline and ends the call", async () => {
    const { control, media, session } = setup();
    session.onOffer("alice", "call-1", "r1", "voice");
    await session.decline();
    expect(control.declined).toEqual(["r1"]);
    expect(session.getState()).toMatchObject({ phase: "ended", endReason: "declined" });
    expect(media.connects).toHaveLength(0);
  });

  it("caller: a declined ring ends the call without connecting media", async () => {
    const { media, session } = setup();
    await session.startCall("bob", "voice");
    await session.onRing("declined");
    expect(session.getState()).toMatchObject({ phase: "ended", endReason: "declined" });
    expect(media.connects).toHaveLength(0);
  });

  it("hangup tears down media", async () => {
    const { media, session } = setup();
    await session.startCall("bob", "voice");
    await session.onRing("answered");
    await session.hangup();
    expect(session.getState().phase).toBe("ended");
    expect(media.disconnects).toBeGreaterThanOrEqual(1);
  });

  it("a media failure ends the call and disconnects", async () => {
    const { media, session } = setup(true);
    session.onOffer("alice", "call-1", "r1", "voice");
    await session.accept();
    expect(session.getState()).toMatchObject({ phase: "ended", endReason: "media_failed" });
    expect(media.disconnects).toBeGreaterThanOrEqual(1);
  });

  it("onRemoteEnd ends the call and disconnects media", async () => {
    const { media, session } = setup();
    await session.startCall("bob", "voice");
    await session.onRing("answered");
    await session.onRemoteEnd("peer_gone");
    expect(session.getState()).toMatchObject({ phase: "ended", endReason: "peer_gone" });
    expect(media.disconnects).toBeGreaterThanOrEqual(1);
  });
});
