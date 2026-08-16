import { describe, expect, it } from "vitest";
import { BreakoutController, type RoomJoiner } from "./breakoutRooms";

class FakeJoiner implements RoomJoiner {
  joined: Array<{ room: string; token: string }> = [];
  join(room: string, token: string): Promise<void> {
    this.joined.push({ room, token });
    return Promise.resolve();
  }
}

describe("BreakoutController", () => {
  it("joins the assigned room and rejoins on a move", async () => {
    const joiner = new FakeJoiner();
    const c = new BreakoutController(joiner);

    await c.apply({ roomId: null, room: "main", joinToken: "t-main" });
    await c.apply({ roomId: "r1", room: "bo-1", joinToken: "t-bo1" });
    await c.apply({ roomId: null, room: "main", joinToken: "t-main2" });

    expect(joiner.joined.map((j) => j.room)).toEqual(["main", "bo-1", "main"]);
    expect(c.getState()).toEqual({ roomId: null, room: "main" });
  });

  it("is a no-op when the room does not change (repeated polls)", async () => {
    const joiner = new FakeJoiner();
    const c = new BreakoutController(joiner);
    await c.apply({ roomId: "r1", room: "bo-1", joinToken: "t1" });
    await c.apply({ roomId: "r1", room: "bo-1", joinToken: "t1-again" });
    expect(joiner.joined).toHaveLength(1);
  });
});
