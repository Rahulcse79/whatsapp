import { describe, expect, it } from "vitest";
import { MultiCameraController, type CameraEnumerator, type CameraInfo, type SecondaryCamera } from "./multiCamera";

class FakeEnum implements CameraEnumerator {
  constructor(private cams: CameraInfo[]) {}
  list(): Promise<CameraInfo[]> {
    return Promise.resolve(this.cams);
  }
}

class FakeSecondary implements SecondaryCamera {
  log: string[] = [];
  publish(deviceId: string): Promise<void> {
    this.log.push(`publish:${deviceId}`);
    return Promise.resolve();
  }
  unpublish(): Promise<void> {
    this.log.push("unpublish");
    return Promise.resolve();
  }
}

const cams: CameraInfo[] = [
  { deviceId: "front", label: "Front" },
  { deviceId: "back", label: "Back" },
];

describe("MultiCameraController", () => {
  it("enumerates and defaults the primary to the first camera", async () => {
    const c = new MultiCameraController(new FakeEnum(cams), new FakeSecondary());
    const list = await c.refresh();
    expect(list).toHaveLength(2);
    expect(c.getState().primaryId).toBe("front");
  });

  it("publishes a distinct secondary camera and stops it", async () => {
    const sec = new FakeSecondary();
    const c = new MultiCameraController(new FakeEnum(cams), sec);
    await c.refresh();
    await c.enableSecondary("back");
    expect(c.getState().secondaryId).toBe("back");
    expect(sec.log).toEqual(["publish:back"]);
    await c.disableSecondary();
    expect(c.getState().secondaryId).toBeNull();
    expect(sec.log).toEqual(["publish:back", "unpublish"]);
  });

  it("rejects a secondary that equals the primary", async () => {
    const c = new MultiCameraController(new FakeEnum(cams), new FakeSecondary());
    await c.refresh(); // primary = front
    await expect(c.enableSecondary("front")).rejects.toThrow(/differ/);
  });

  it("drops the secondary when the primary switches onto it", async () => {
    const sec = new FakeSecondary();
    const c = new MultiCameraController(new FakeEnum(cams), sec);
    await c.refresh();
    await c.enableSecondary("back");
    await c.selectPrimary("back");
    expect(c.getState()).toMatchObject({ primaryId: "back", secondaryId: null });
    expect(sec.log).toContain("unpublish");
  });
});
