// On-device messaging AI (T11.02). Smart replies, grammar correction, and
// extractive summarization run entirely on-device with deterministic logic — no
// model, nothing leaves the device. Model-backed features (translation, free-form
// generation) and browser-API features (voice↔text) route through the T11.01
// AiRuntime / platform ports instead. Pure + framework-free.

// ── smart replies ────────────────────────────────────────────────────────────

const GREETING = /\b(hi|hello|hey|yo|howdy|good (morning|afternoon|evening))\b/i;
const GRATITUDE = /\b(thanks|thank you|thx|appreciate it|cheers)\b/i;
const APOLOGY = /\b(sorry|my bad|apologi[sz]e)\b/i;
const FAREWELL = /\b(bye|goodbye|see you|see ya|good night|gtg|talk later)\b/i;

/** smartReplies suggests up to 3 quick replies for a received message, from a
 *  small on-device intent classifier. Returns [] when there's nothing to suggest. */
export function smartReplies(lastReceived: string): string[] {
  const t = lastReceived.trim();
  if (t === "") return [];
  const out: string[] = [];
  const push = (...xs: string[]): void => {
    for (const x of xs) if (!out.includes(x) && out.length < 3) out.push(x);
  };

  if (GRATITUDE.test(t)) push("You're welcome! 🙂", "No problem 👍", "Anytime");
  else if (APOLOGY.test(t)) push("No worries!", "It's all good 👍", "Don't worry about it");
  else if (FAREWELL.test(t)) push("See you! 👋", "Bye!", "Talk soon");
  else if (GREETING.test(t) && t.length < 40) push("Hey! 👋", "Hi there", "Hello!");
  else if (t.endsWith("?")) push("Yes", "No", "Let me check 🤔");
  else push("👍", "Sounds good!", "Got it");

  return out;
}

// ── grammar correction ───────────────────────────────────────────────────────

const TYPOS: Record<string, string> = {
  teh: "the",
  recieve: "receive",
  definately: "definitely",
  seperate: "separate",
  occured: "occurred",
  wich: "which",
  becuase: "because",
  alot: "a lot",
  wanna: "want to",
  gonna: "going to",
};

export interface GrammarResult {
  text: string;
  changed: boolean;
}

/** correctGrammar applies safe, deterministic on-device fixes: common typos, the
 *  pronoun "i" → "I", sentence-initial capitalization, collapsed double spaces,
 *  and a terminal period. Conservative — it never rewrites meaning. */
export function correctGrammar(input: string): GrammarResult {
  let s = input;

  // common typos (word-boundary, case-insensitive → lowercase replacement)
  for (const [wrong, right] of Object.entries(TYPOS)) {
    s = s.replace(new RegExp(`\\b${wrong}\\b`, "gi"), right);
  }
  // "i" pronoun (also fixes i'm / i'll / i've via the word boundary)
  s = s.replace(/\bi\b/g, "I");
  // capitalize the first letter of each sentence
  s = s.replace(/(^\s*|[.!?]\s+)([a-z])/g, (_m, lead: string, ch: string) => lead + ch.toUpperCase());
  // collapse runs of spaces (but keep newlines)
  s = s.replace(/ {2,}/g, " ");
  // ensure terminal punctuation on a single-line message that lacks it
  if (s.trim() !== "" && !/[.!?…]$/.test(s.trimEnd()) && !s.includes("\n")) {
    s = s.trimEnd() + ".";
  }

  return { text: s, changed: s !== input };
}

// ── extractive summarization ─────────────────────────────────────────────────

const STOPWORDS = new Set(
  "the a an and or but if then of to in on for with at by from as is are was were be been being it this that these those i you he she we they me him her them my your our their so not no yes do does did have has had will would can could should".split(
    " ",
  ),
);

function splitSentences(text: string): string[] {
  return text
    .replace(/\s+/g, " ")
    .split(/(?<=[.!?])\s+/)
    .map((x) => x.trim())
    .filter((x) => x.length > 0);
}

/** summarizeExtractive returns the `maxSentences` most representative sentences
 *  (word-frequency scored, in original order) — a fully on-device summary. */
export function summarizeExtractive(text: string, maxSentences = 3): string {
  const sentences = splitSentences(text);
  if (sentences.length <= maxSentences) return sentences.join(" ");

  // term frequencies over non-stopwords
  const freq = new Map<string, number>();
  for (const s of sentences) {
    for (const w of s.toLowerCase().match(/[a-z0-9']+/g) ?? []) {
      if (!STOPWORDS.has(w) && w.length > 2) freq.set(w, (freq.get(w) ?? 0) + 1);
    }
  }
  // score each sentence by mean word weight (length-normalised so long sentences
  // don't dominate)
  const scored = sentences.map((s, i) => {
    const words = s.toLowerCase().match(/[a-z0-9']+/g) ?? [];
    const score = words.reduce((sum, w) => sum + (freq.get(w) ?? 0), 0) / Math.max(1, words.length);
    return { i, s, score };
  });
  const top = [...scored]
    .sort((a, b) => b.score - a.score)
    .slice(0, maxSentences)
    .sort((a, b) => a.i - b.i) // restore reading order
    .map((x) => x.s);
  return top.join(" ");
}

/** summarizeConversation flattens recent "sender: text" lines and summarizes them. */
export function summarizeConversation(lines: string[], maxSentences = 3): string {
  return summarizeExtractive(lines.join(". "), maxSentences);
}
