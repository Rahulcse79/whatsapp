import { describe, expect, it } from "vitest";
import { encodeInteractive, parseInteractive, replyTextFor, validateInteractive, type InteractiveButton } from "./interactive";

const btns = (n: number): InteractiveButton[] =>
  Array.from({ length: n }, (_, i) => ({ id: `b${i}`, label: `Button ${i}` }));

describe("validateInteractive", () => {
  it("requires text and 1–3 buttons", () => {
    expect(validateInteractive("", btns(1))).toMatch(/text/i);
    expect(validateInteractive("hi", [])).toMatch(/between 1 and 3/i);
    expect(validateInteractive("hi", btns(4))).toMatch(/between 1 and 3/i);
    expect(validateInteractive("hi", btns(2))).toBeNull();
  });
  it("validates url buttons", () => {
    expect(validateInteractive("hi", [{ id: "b", label: "Open", kind: "url", url: "notaurl" }])).toMatch(/https/i);
    expect(validateInteractive("hi", [{ id: "b", label: "Open", kind: "url", url: "https://x.com" }])).toBeNull();
  });
});

describe("encode/parse round-trip", () => {
  it("round-trips a valid interactive message", () => {
    const body = encodeInteractive("Pick one", [
      { id: "y", label: "Yes", payload: "YES" },
      { id: "n", label: "No" },
    ]);
    const m = parseInteractive(body);
    expect(m?.t).toBe("interactive");
    expect(m?.buttons.map((b) => b.label)).toEqual(["Yes", "No"]);
    expect(m?.buttons[0]!.kind).toBe("reply"); // defaulted
  });
  it("throws on invalid encode", () => {
    expect(() => encodeInteractive("", btns(1))).toThrow();
  });
  it("returns null for non-interactive or malformed bodies", () => {
    expect(parseInteractive('{"t":"text","text":"hi"}')).toBeNull();
    expect(parseInteractive("not json")).toBeNull();
    expect(parseInteractive('{"t":"interactive","text":"x","buttons":[]}')).toBeNull();
  });
  it("drops malformed buttons but keeps valid ones", () => {
    const m = parseInteractive('{"t":"interactive","text":"x","buttons":[{"id":"a","label":"OK"},{"label":""},{"nope":1}]}');
    expect(m?.buttons.map((b) => b.label)).toEqual(["OK"]);
  });
});

describe("replyTextFor", () => {
  it("prefers payload, falls back to label", () => {
    expect(replyTextFor({ id: "a", label: "Yes", payload: "YES" })).toBe("YES");
    expect(replyTextFor({ id: "a", label: "Maybe" })).toBe("Maybe");
  });
});
