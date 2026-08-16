import { describe, expect, it } from "vitest";
import { analyzeLink, analyzeMessageLinks, extractUrls, scoreMessage } from "./abuse";

describe("analyzeLink", () => {
  it("passes an ordinary https link", () => {
    expect(analyzeLink({ href: "https://example.com/page" }).risk).toBe("safe");
  });
  it("flags punycode / homograph hosts as danger", () => {
    const v = analyzeLink({ href: "https://xn--pple-43d.com/login" });
    expect(v.risk).toBe("danger");
    expect(v.reasons.join(" ")).toMatch(/look-alike/);
  });
  it("flags text/href mismatch as danger", () => {
    const v = analyzeLink({ href: "https://evil.example/login", text: "paypal.com" });
    expect(v.risk).toBe("danger");
    expect(v.reasons.join(" ")).toMatch(/paypal\.com/);
  });
  it("cautions on shorteners, raw IPs, and hidden usernames", () => {
    expect(analyzeLink({ href: "https://bit.ly/abc" }).risk).toBe("caution");
    expect(analyzeLink({ href: "http://192.168.1.1/setup" }).risk).toBe("caution");
    expect(analyzeLink({ href: "https://user@evil.example/" }).risk).toBe("danger");
  });
  it("does not flag matching text/href on the same site", () => {
    expect(analyzeLink({ href: "https://mail.example.com/x", text: "example.com" }).risk).toBe("safe");
  });
});

describe("extractUrls + analyzeMessageLinks", () => {
  it("extracts urls and returns the worst verdict", () => {
    const text = "hey see https://example.com and https://xn--pple-43d.com/login now";
    expect(extractUrls(text)).toHaveLength(2);
    expect(analyzeMessageLinks(text).risk).toBe("danger");
  });
  it("is safe when there are no links", () => {
    expect(analyzeMessageLinks("just a normal message").risk).toBe("safe");
  });
});

describe("scoreMessage", () => {
  it("is none for a normal message from a contact", () => {
    expect(scoreMessage({ text: "lunch at 1?", fromContact: true, isFirstMessage: false }).level).toBe("none");
  });
  it("is high for scam wording + risky link from a stranger", () => {
    const v = scoreMessage({ text: "CONGRATULATIONS you'​ve won a FREE gift card, claim now", fromContact: false, isFirstMessage: true, hasRiskyLink: true });
    expect(v.level).toBe("high");
  });
  it("is low for a first message from an unknown sender", () => {
    expect(scoreMessage({ text: "hi", fromContact: false, isFirstMessage: true }).level).toBe("low");
  });
});
