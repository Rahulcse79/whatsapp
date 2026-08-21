import { describe, expect, it } from "vitest";
import { correctGrammar, smartReplies, summarizeConversation, summarizeExtractive } from "./messagingAi";

describe("smartReplies", () => {
  it("answers a question with yes/no/maybe", () => {
    expect(smartReplies("are you free tonight?")).toEqual(["Yes", "No", "Let me check 🤔"]);
  });
  it("greets back a greeting", () => {
    expect(smartReplies("hey!")[0]).toBe("Hey! 👋");
  });
  it("acknowledges thanks and apologies", () => {
    expect(smartReplies("thanks so much")[0]).toMatch(/welcome/i);
    expect(smartReplies("sorry about that")[0]).toMatch(/no worries/i);
  });
  it("falls back to generic acks and returns nothing for empty input", () => {
    expect(smartReplies("the package arrived").length).toBeGreaterThan(0);
    expect(smartReplies("   ")).toEqual([]);
  });
});

describe("correctGrammar", () => {
  it("fixes the pronoun i, typos, and capitalization", () => {
    const r = correctGrammar("i think teh meeting is at 3");
    expect(r.text).toBe("I think the meeting is at 3.");
    expect(r.changed).toBe(true);
  });
  it("fixes i'm / i'll via the word boundary", () => {
    expect(correctGrammar("i'm on my way").text).toBe("I'm on my way.");
  });
  it("collapses double spaces and adds terminal punctuation", () => {
    expect(correctGrammar("hello   world").text).toBe("Hello world.");
  });
  it("reports no change for already-correct text", () => {
    expect(correctGrammar("See you tomorrow!").changed).toBe(false);
  });
  it("leaves multi-line messages without forcing a period", () => {
    const r = correctGrammar("line one\nline two");
    expect(r.text.endsWith(".")).toBe(false);
  });
});

describe("summarizeExtractive", () => {
  it("returns the text unchanged when short", () => {
    expect(summarizeExtractive("Just one sentence.", 3)).toBe("Just one sentence.");
  });
  it("picks the most representative sentences in reading order", () => {
    const text =
      "The project deadline is Friday. The project needs the report and the budget. Weather is nice today. The report and budget are due to the project lead by Friday.";
    const out = summarizeExtractive(text, 2);
    // the two project/report/budget sentences should win over the weather aside
    expect(out).not.toMatch(/Weather/);
    expect(out.split(". ").length).toBeLessThanOrEqual(2 + 1);
  });
  it("summarizes conversation lines", () => {
    const out = summarizeConversation(["Alice: dinner at 7", "Bob: works for me", "Alice: I'll book the table"], 2);
    expect(out.length).toBeGreaterThan(0);
  });
});
