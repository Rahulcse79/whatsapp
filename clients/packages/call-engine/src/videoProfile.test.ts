import { describe, expect, it } from "vitest";
import { profileForBudget, VideoProfileController, VIDEO_PROFILES, type VideoConstrainer, type VideoProfile } from "./videoProfile";

class FakeConstrainer implements VideoConstrainer {
  applied: string[] = [];
  apply(p: VideoProfile): Promise<void> {
    this.applied.push(p.id);
    return Promise.resolve();
  }
}

describe("profileForBudget", () => {
  it("picks the highest tier that fits the uplink", () => {
    expect(profileForBudget(500).id).toBe("360p");
    expect(profileForBudget(2000).id).toBe("720p");
    expect(profileForBudget(4000).id).toBe("1080p");
  });

  it("gates 4K behind allow4k", () => {
    expect(profileForBudget(20000).id).toBe("1080p"); // 4K not allowed by default
    expect(profileForBudget(20000, true).id).toBe("4k");
  });
});

describe("VideoProfileController", () => {
  it("applies a chosen profile once and dedupes repeats", async () => {
    const con = new FakeConstrainer();
    const c = new VideoProfileController(con, "720p");
    await c.set("1080p");
    await c.set("1080p"); // no-op
    expect(con.applied).toEqual(["1080p"]);
    expect(c.getState().profile).toEqual(VIDEO_PROFILES["1080p"]);
  });

  it("refuses 4K until opted in", async () => {
    const con = new FakeConstrainer();
    const c = new VideoProfileController(con);
    await expect(c.set("4k")).rejects.toThrow(/allow4k/);
    c.setAllow4k(true);
    await c.set("4k");
    expect(con.applied).toEqual(["4k"]);
  });

  it("adapts to the available budget", async () => {
    const con = new FakeConstrainer();
    const c = new VideoProfileController(con, "360p", true);
    await c.adapt(20000);
    expect(c.getState().profile.id).toBe("4k");
  });
});
