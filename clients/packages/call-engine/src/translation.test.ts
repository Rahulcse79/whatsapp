import { describe, expect, it } from "vitest";
import type { CaptionLine } from "./captions";
import { TranslationController, type Translator } from "./translation";

function fakeTranslator(): Translator & { calls: number } {
  const t = {
    calls: 0,
    translate(text: string, lang: string) {
      t.calls++;
      return Promise.resolve(`[${lang}] ${text}`);
    },
  };
  return t;
}

const line = (text: string): CaptionLine => ({ id: "1", speakerId: "p", text, final: true, ts: 1 });

describe("TranslationController (T9.02)", () => {
  it("returns the original text when translation is off", async () => {
    const c = new TranslationController(fakeTranslator());
    expect(c.isEnabled()).toBe(false);
    expect(await c.translateLine(line("hello"))).toBe("hello");
  });

  it("translates into the target language", async () => {
    const c = new TranslationController(fakeTranslator());
    c.setTargetLang("es");
    expect(c.isEnabled()).toBe(true);
    expect(await c.translateLine(line("hello"))).toBe("[es] hello");
  });

  it("caches per (lang, text) so repeats don't re-translate", async () => {
    const t = fakeTranslator();
    const c = new TranslationController(t);
    c.setTargetLang("fr");
    await c.translateLine(line("hi"));
    await c.translateLine(line("hi"));
    expect(t.calls).toBe(1); // second was cached
    c.setTargetLang("de");
    await c.translateLine(line("hi"));
    expect(t.calls).toBe(2); // new language → re-translate
  });
});
