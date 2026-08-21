// On-device AI moderation + assistant primitives (T11.03). Toxicity detection,
// intent classification, auto-tagging, meeting-style summaries, and a local
// "semantic" (relevance) rank over decrypted history — all deterministic and
// on-device (nothing leaves the device). Model-backed free-form assistant replies
// route through the T11.01 AiRuntime provider (a seam). Pure + framework-free.

import { summarizeExtractive } from "./messagingAi";

// ── toxicity ─────────────────────────────────────────────────────────────────

export type ToxicityLevel = "none" | "low" | "high";

export interface ToxicityResult {
  level: ToxicityLevel;
  categories: string[]; // e.g. "threat", "insult", "profanity"
}

const THREATS = /\b(kill|hurt|beat|attack|destroy|end) (you|him|her|them|u)\b|\bi('| a)?m going to (get|hurt|kill)\b/i;
const INSULTS = /\b(idiot|stupid|moron|loser|dumb|pathetic|worthless|shut up|i hate you|you're? (an? )?(idiot|loser|fool))\b/i;
const PROFANITY = /\b(damn|hell|crap|screw (you|off)|jerk)\b/i;

/** detectToxicity flags abusive language on-device (used to warn before sending
 *  and to soften surfacing of hostile inbound messages). Coarse by design — it
 *  errs toward "low", and a real classifier can replace it at the same seam. */
export function detectToxicity(text: string): ToxicityResult {
  const categories: string[] = [];
  if (THREATS.test(text)) categories.push("threat");
  if (INSULTS.test(text)) categories.push("insult");
  if (PROFANITY.test(text)) categories.push("profanity");

  let level: ToxicityLevel = "none";
  if (categories.includes("threat") || categories.includes("insult")) level = "high";
  else if (categories.length > 0) level = "low";
  return { level, categories };
}

// ── intent detection ─────────────────────────────────────────────────────────

export type Intent = "question" | "request" | "scheduling" | "greeting" | "farewell" | "gratitude" | "statement";

const TIME_HINT = /\b(\d{1,2}(:\d{2})?\s?(am|pm)?|today|tomorrow|tonight|monday|tuesday|wednesday|thursday|friday|saturday|sunday|next week)\b/i;
const SCHEDULE_HINT = /\b(meet|meeting|call|schedule|catch up|appointment|reschedule|lunch|dinner|coffee)\b/i;
const REQUEST_HINT = /\b(can|could|would|will) you\b|\bplease\b|\bcould you\b|\bcan you\b|\bneed you to\b/i;
const QUESTION_START = /^(who|what|when|where|why|how|which|do|does|did|is|are|can|could|would|should|will)\b/i;

/** detectIntent classifies a message's purpose on-device (drives smart replies,
 *  auto-tags, and assistant routing). */
export function detectIntent(text: string): Intent {
  const t = text.trim();
  if (t === "") return "statement";
  if (/\b(thanks|thank you|thx|appreciate it)\b/i.test(t)) return "gratitude";
  if (/\b(bye|goodbye|see you|see ya|good night|talk later)\b/i.test(t)) return "farewell";
  if (/^\s*(hi|hello|hey|yo|good (morning|afternoon|evening))\b/i.test(t)) return "greeting";
  if (SCHEDULE_HINT.test(t) && (TIME_HINT.test(t) || /\b(let's|lets|shall we|when)\b/i.test(t))) return "scheduling";
  if (REQUEST_HINT.test(t)) return "request";
  if (t.endsWith("?") || QUESTION_START.test(t)) return "question";
  return "statement";
}

// ── auto-tagging ─────────────────────────────────────────────────────────────

const TAG_LEXICON: Record<string, RegExp> = {
  work: /\b(project|deadline|meeting|report|client|budget|invoice|email|task|deliverable|standup)\b/i,
  money: /\b(pay|payment|invoice|budget|cost|price|\$|refund|salary|transfer|owe|bill)\b/i,
  travel: /\b(flight|hotel|trip|travel|airport|booking|train|visa|itinerary|passport)\b/i,
  scheduling: /\b(meet|meeting|call|schedule|appointment|calendar|reschedule|tomorrow|tonight)\b/i,
  food: /\b(lunch|dinner|breakfast|coffee|restaurant|eat|food|pizza|reservation)\b/i,
  social: /\b(party|birthday|weekend|hang out|drinks|movie|game|concert)\b/i,
  health: /\b(doctor|appointment|sick|medicine|gym|workout|hospital|dentist)\b/i,
  urgent: /\b(urgent|asap|now|immediately|emergency|right away|deadline)\b/i,
};

/** autoTags returns the topic tags a message matches (on-device keyword classes). */
export function autoTags(text: string): string[] {
  const tags: string[] = [];
  for (const [tag, re] of Object.entries(TAG_LEXICON)) if (re.test(text)) tags.push(tag);
  return tags;
}

// ── meeting / conversation summary ───────────────────────────────────────────

export interface MeetingSummary {
  summary: string;
  actionItems: string[];
  topics: string[];
}

const ACTION_HINT = /\b(let's|lets|i'll|we'll|need to|have to|will|please|todo|action item|follow up|by (monday|tuesday|wednesday|thursday|friday|saturday|sunday|tomorrow|tonight|\d))/i;

/** meetingSummary extracts a short summary, action items, and topics from a set
 *  of "speaker: text" lines — a fully on-device meeting recap. */
export function meetingSummary(lines: string[]): MeetingSummary {
  const clean = lines.map((l) => l.trim()).filter((l) => l.length > 0);
  const summary = clean.length ? summarizeExtractive(clean.join(". "), 4) : "";
  const actionItems: string[] = [];
  const topicSet = new Set<string>();
  for (const l of clean) {
    if (ACTION_HINT.test(l)) actionItems.push(l);
    for (const tag of autoTags(l)) topicSet.add(tag);
  }
  return { summary, actionItems: actionItems.slice(0, 8), topics: [...topicSet] };
}

// ── local "semantic" search over history ─────────────────────────────────────

export interface RankedDoc {
  id: string;
  score: number;
}

const STOP = new Set("the a an and or but of to in on for with at by is are was were be it this that i you we they".split(" "));

function tokens(s: string): string[] {
  return (s.toLowerCase().match(/[a-z0-9']+/g) ?? []).filter((w) => w.length > 1 && !STOP.has(w));
}

/** rankBySimilarity ranks docs by relevance to a query using a term-frequency
 *  cosine restricted to query terms (weighting rarer terms higher via a coarse
 *  IDF). A local approximation of semantic search — embeddings ride the model
 *  seam. Returns docs with score > 0, best first, capped at `limit`. */
export function rankBySimilarity(query: string, docs: { id: string; text: string }[], limit = 20): RankedDoc[] {
  const qTerms = tokens(query);
  if (qTerms.length === 0) return [];
  // document frequency per query term
  const df = new Map<string, number>();
  const docTokens = docs.map((d) => {
    const tks = new Set(tokens(d.text));
    for (const q of qTerms) if (tks.has(q)) df.set(q, (df.get(q) ?? 0) + 1);
    return tks;
  });
  const n = docs.length || 1;
  const scored: RankedDoc[] = docs.map((d, i) => {
    let score = 0;
    for (const q of qTerms) {
      if (docTokens[i]!.has(q)) {
        const idf = Math.log(1 + n / (1 + (df.get(q) ?? 0)));
        score += idf;
      }
    }
    return { id: d.id, score };
  });
  return scored
    .filter((s) => s.score > 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, limit);
}

// ── assistant routing ────────────────────────────────────────────────────────

export type AssistantAction =
  | { kind: "summarize" }
  | { kind: "search"; query: string }
  | { kind: "answer"; text: string }
  | { kind: "model" }; // free-form → route through the AiRuntime provider (seam)

/** assistantIntent maps a user's assistant query to an on-device action where
 *  possible (summarize / search / canned answer), else defers to a model. */
export function assistantIntent(query: string): AssistantAction {
  const q = query.trim().toLowerCase();
  if (q === "") return { kind: "answer", text: "Ask me to summarize this chat, or search your messages." };
  if (/\b(summari[sz]e|recap|tl;?dr|key points)\b/.test(q)) return { kind: "summarize" };
  const m = q.match(/\b(?:search|find|look for|show me)\b\s+(?:for\s+)?(.*)/);
  if (m && m[1]) return { kind: "search", query: m[1] };
  if (/\b(help|what can you do)\b/.test(q)) {
    return { kind: "answer", text: "I can summarize this chat and search your messages — all on your device." };
  }
  return { kind: "model" };
}
