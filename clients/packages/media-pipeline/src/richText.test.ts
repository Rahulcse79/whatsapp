import { describe, expect, it } from "vitest";
import { tokenizeRich } from "./richText";

describe("tokenizeRich (T6.01 rich text)", () => {
  it("returns a single text token for plain text", () => {
    expect(tokenizeRich("just text")).toEqual([{ t: "text", v: "just text" }]);
  });

  it("parses the four inline markers", () => {
    expect(tokenizeRich("*b*")).toEqual([{ t: "b", v: "b" }]);
    expect(tokenizeRich("_i_")).toEqual([{ t: "i", v: "i" }]);
    expect(tokenizeRich("~s~")).toEqual([{ t: "s", v: "s" }]);
    expect(tokenizeRich("`c`")).toEqual([{ t: "code", v: "c" }]);
  });

  it("mixes plain text and markers in order", () => {
    expect(tokenizeRich("a *bold* and _it_ end")).toEqual([
      { t: "text", v: "a " },
      { t: "b", v: "bold" },
      { t: "text", v: " and " },
      { t: "i", v: "it" },
      { t: "text", v: " end" },
    ]);
  });

  it("extracts a fenced code block verbatim", () => {
    expect(tokenizeRich("see ```\ncode *not bold*\n``` done")).toEqual([
      { t: "text", v: "see " },
      { t: "pre", v: "code *not bold*" },
      { t: "text", v: " done" },
    ]);
  });

  it("autolinks http(s) URLs and trims trailing punctuation", () => {
    expect(tokenizeRich("go https://example.com/x, now")).toEqual([
      { t: "text", v: "go " },
      { t: "link", v: "https://example.com/x" },
      { t: "text", v: ", now" },
    ]);
  });

  it("leaves an unmatched marker as literal text", () => {
    expect(tokenizeRich("2 * 3 = 6")).toEqual([{ t: "text", v: "2 * 3 = 6" }]);
  });
});
