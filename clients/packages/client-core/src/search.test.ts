import { describe, expect, it } from "vitest";
import { SNIPPET_CLOSE, SNIPPET_OPEN, matchMemory, toFtsQuery, tokenize } from "./search";

const wrap = (s: string): string => `${SNIPPET_OPEN}${s}${SNIPPET_CLOSE}`;

describe("tokenize", () => {
  it("lowercases and splits on non-word characters", () => {
    expect(tokenize("Hello, WORLD!")).toEqual(["hello", "world"]);
  });
  it("keeps unicode letters and digits, drops punctuation/emoji", () => {
    expect(tokenize("café 42 — dör 🌍 test")).toEqual(["café", "42", "dör", "test"]);
  });
  it("returns [] for whitespace/punctuation-only input", () => {
    expect(tokenize("   … !! ")).toEqual([]);
  });
});

describe("toFtsQuery", () => {
  it("builds a quoted prefix term per token, implicitly ANDed", () => {
    expect(toFtsQuery("hello wor")).toBe('"hello"* "wor"*');
  });
  it("neutralises FTS operators and punctuation by quoting", () => {
    // `OR`, `-`, `*`, `"` never reach the parser as syntax — only as content.
    expect(toFtsQuery('foo OR -bar* "x"')).toBe('"foo"* "or"* "bar"* "x"*');
  });
  it("returns '' for empty / punctuation-only input (caller skips)", () => {
    expect(toFtsQuery("   ")).toBe("");
    expect(toFtsQuery("!!!")).toBe("");
  });
});

describe("matchMemory", () => {
  it("requires every token to prefix-match some word (implicit AND)", () => {
    expect(matchMemory("the quick brown fox", ["quick", "brown"]).matched).toBe(true);
    expect(matchMemory("the quick brown fox", ["quick", "zzz"]).matched).toBe(false);
  });

  it("matches by prefix, not just whole words", () => {
    const r = matchMemory("meeting tomorrow", ["meet"]);
    expect(r.matched).toBe(true);
    expect(r.snippet).toContain(wrap("meeting"));
  });

  it("scores more/repeated hits higher (drives ranking order)", () => {
    const many = matchMemory("dinner dinner plans for dinner", ["dinner"]).score;
    const few = matchMemory("dinner plans", ["dinner"]).score;
    expect(many).toBeGreaterThan(few);
  });

  it("highlights matched words and preserves original text in the window", () => {
    const r = matchMemory("let us meet at the park at noon", ["meet"]);
    expect(r.snippet).toContain(`us ${wrap("meet")} at`);
  });

  it("windows long bodies around the first hit with ellipses", () => {
    const body = Array.from({ length: 40 }, (_, i) => `w${i}`).join(" ") + " needle tail";
    const r = matchMemory(body, ["needle"], 6);
    expect(r.matched).toBe(true);
    expect(r.snippet.startsWith("…")).toBe(true);
    expect(r.snippet).toContain(wrap("needle"));
  });

  it("does not prepend an ellipsis when the hit is at the start", () => {
    const r = matchMemory("needle in a haystack somewhere", ["needle"], 4);
    expect(r.snippet.startsWith("…")).toBe(false);
  });

  it("returns no match for an empty token list", () => {
    expect(matchMemory("anything", [])).toEqual({ matched: false, score: 0, snippet: "" });
  });
});
