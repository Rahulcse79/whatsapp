import { describe, expect, it } from "vitest";
import { assistantIntent, autoTags, detectIntent, detectToxicity, meetingSummary, rankBySimilarity } from "./aiModeration";

describe("detectToxicity", () => {
  it("flags threats and insults as high", () => {
    expect(detectToxicity("i'm going to hurt you").level).toBe("high");
    expect(detectToxicity("you are an idiot").level).toBe("high");
  });
  it("flags mild profanity as low", () => {
    expect(detectToxicity("this is a damn mess").level).toBe("low");
  });
  it("passes normal text", () => {
    const r = detectToxicity("see you at lunch tomorrow");
    expect(r.level).toBe("none");
    expect(r.categories).toEqual([]);
  });
});

describe("detectIntent", () => {
  it("classifies the common intents", () => {
    expect(detectIntent("can you send the file please")).toBe("request");
    expect(detectIntent("what time is the meeting?")).toBe("question");
    expect(detectIntent("let's meet tomorrow at 3pm")).toBe("scheduling");
    expect(detectIntent("thanks a lot")).toBe("gratitude");
    expect(detectIntent("hey there")).toBe("greeting");
    expect(detectIntent("the report is done")).toBe("statement");
  });
});

describe("autoTags", () => {
  it("tags topics from keywords", () => {
    expect(autoTags("can we reschedule the client meeting, it's urgent").sort()).toEqual(["scheduling", "urgent", "work"]);
    expect(autoTags("nothing special here")).toEqual([]);
  });
});

describe("meetingSummary", () => {
  it("extracts a summary, action items, and topics", () => {
    const lines = [
      "Alice: the project deadline is Friday",
      "Bob: I'll send the budget report by tomorrow",
      "Alice: let's meet at 3pm to review",
      "Bob: weather is nice",
    ];
    const s = meetingSummary(lines);
    expect(s.summary.length).toBeGreaterThan(0);
    expect(s.actionItems.some((a) => /budget report/i.test(a))).toBe(true);
    expect(s.topics).toContain("work");
  });
});

describe("rankBySimilarity", () => {
  const docs = [
    { id: "1", text: "let's book flights and a hotel for the trip" },
    { id: "2", text: "the project budget report is due Friday" },
    { id: "3", text: "dinner reservation at 7" },
  ];
  it("ranks by relevance, rarer terms weighing more", () => {
    const r = rankBySimilarity("budget report", docs);
    expect(r[0]!.id).toBe("2");
  });
  it("returns nothing for an empty or unmatched query", () => {
    expect(rankBySimilarity("", docs)).toEqual([]);
    expect(rankBySimilarity("spaceship", docs)).toEqual([]);
  });
});

describe("assistantIntent", () => {
  it("routes on-device actions and defers free-form to a model", () => {
    expect(assistantIntent("summarize this chat")).toEqual({ kind: "summarize" });
    expect(assistantIntent("search for the invoice")).toEqual({ kind: "search", query: "the invoice" });
    expect(assistantIntent("what can you do")).toMatchObject({ kind: "answer" });
    expect(assistantIntent("write me a poem about the sea")).toEqual({ kind: "model" });
  });
});
