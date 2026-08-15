// Real-time caption translation (T9.02). Translation runs on-device (web/mobile
// on-device model) so caption text is never sent to a translation service. Pure
// control over a Translator port, with a per-(lang,text) cache so a repeated or
// re-rendered line isn't re-translated.

import type { CaptionLine } from "./captions";

/** Translator turns text into the target language, on-device. */
export interface Translator {
  translate(text: string, targetLang: string): Promise<string>;
}

export class TranslationController {
  private targetLang: string | null = null;
  private readonly cache = new Map<string, string>(); // `${lang}\n${text}` → translated

  constructor(private readonly translator: Translator) {}

  /** setTargetLang chooses the language to translate captions into; null = off. */
  setTargetLang(lang: string | null): void {
    this.targetLang = lang;
  }
  targetLanguage(): string | null {
    return this.targetLang;
  }
  isEnabled(): boolean {
    return this.targetLang !== null;
  }

  /** translateLine returns the line's text in the target language (cached), or
   *  the original text when translation is off or the line is already in-target. */
  async translateLine(line: CaptionLine): Promise<string> {
    const lang = this.targetLang;
    if (!lang) return line.text;
    const key = `${lang}\n${line.text}`;
    const hit = this.cache.get(key);
    if (hit !== undefined) return hit;
    const out = await this.translator.translate(line.text, lang);
    this.cache.set(key, out);
    return out;
  }
}
