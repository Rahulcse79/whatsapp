// Client-side full-text search helpers, shared by the SQLite FTS5 path
// (MessageStore) and the in-memory path (MemoryMessageRepo). Per ADR-005,
// content search is client-side over the decrypted local store — the server
// holds only ciphertext, so there is nothing to index server-side.
//
// This module is the pure, framework-free core: sanitising raw user input into a
// safe FTS5 MATCH expression, unicode-aware tokenization, and the match/snippet
// scoring the memory path reuses. No SQL, no DOM — unit-tested in Node.

/** One search result, independent of the backing store. */
export interface SearchHit {
  msgUuid: string;
  conversationId: string;
  conversationTitle: string;
  seq: number;
  /** Full decrypted body (for context / navigation). */
  body: string;
  /** Windowed excerpt with matched terms wrapped in the SNIPPET markers below. */
  snippet: string;
  mine: boolean;
  createdAt: number;
}

export interface SearchOptions {
  /** Restrict to one conversation (default: search all conversations). */
  conversationId?: string;
  /** Cap the number of hits (default DEFAULT_SEARCH_LIMIT). */
  limit?: number;
}

export const DEFAULT_SEARCH_LIMIT = 50;

// Markers wrapped around matched terms in a snippet. Both paths use the same two
// so the UI can highlight identically (FTS5 snippet() is handed these literals).
// Chosen from the guillemet block — vanishingly unlikely to occur in chat text.
export const SNIPPET_OPEN = "‹"; // ‹
export const SNIPPET_CLOSE = "›"; // ›

const WORD_RE = /[\p{L}\p{N}]+/gu;

/** tokenize splits text into lowercase word tokens (runs of unicode letters or
 *  numbers), dropping all punctuation and whitespace. */
export function tokenize(text: string): string[] {
  const out: string[] = [];
  for (const m of text.matchAll(WORD_RE)) out.push(m[0].toLowerCase());
  return out;
}

/**
 * toFtsQuery turns raw user input into a safe FTS5 MATCH expression: every search
 * token becomes a double-quoted prefix term (`"term"*`). Quoting means user
 * punctuation and FTS operators (AND/OR/NOT/NEAR/-/(/"/*) can never inject into
 * the query — the token content is always `[\p{L}\p{N}]+`, so it holds no quote
 * to break out with. Tokens are implicitly ANDed. Empty/whitespace-only input
 * yields "" and the caller should skip the query.
 */
export function toFtsQuery(raw: string): string {
  return tokenize(raw)
    .map((t) => `"${t}"*`)
    .join(" ");
}

export interface MemoryMatch {
  matched: boolean;
  score: number;
  snippet: string;
}

interface Word {
  text: string;
  lower: string;
  start: number;
  end: number;
}

function words(body: string): Word[] {
  const out: Word[] = [];
  for (const m of body.matchAll(WORD_RE)) {
    out.push({ text: m[0], lower: m[0].toLowerCase(), start: m.index, end: m.index + m[0].length });
  }
  return out;
}

function isHit(wordLower: string, tokens: string[]): boolean {
  return tokens.some((t) => wordLower.startsWith(t));
}

/**
 * matchMemory scores a body against query tokens for the in-memory path with the
 * same prefix-token AND semantics as toFtsQuery: a body matches iff EVERY token
 * is a prefix of some word in it. Score is the total number of prefix hits, so
 * ranking (higher first) roughly tracks FTS5's frequency-weighted bm25 order.
 * The snippet is a word-window around the first hit, preserving the original text
 * between words and wrapping matched words in the SNIPPET markers, with an
 * ellipsis on either side that is truncated.
 */
export function matchMemory(body: string, tokens: string[], windowWords = 12): MemoryMatch {
  if (tokens.length === 0) return { matched: false, score: 0, snippet: "" };
  const ws = words(body);

  let score = 0;
  for (const t of tokens) {
    let hits = 0;
    for (const w of ws) if (w.lower.startsWith(t)) hits++;
    if (hits === 0) return { matched: false, score: 0, snippet: "" }; // implicit AND
    score += hits;
  }

  const firstIdx = ws.findIndex((w) => isHit(w.lower, tokens));
  const from = Math.max(0, firstIdx - 2);
  const to = Math.min(ws.length, from + windowWords);
  const window = ws.slice(from, to);

  let snip = "";
  let cursor = window[0]?.start ?? 0;
  for (const w of window) {
    snip += body.slice(cursor, w.start); // original inter-word text (spaces/punct)
    snip += isHit(w.lower, tokens) ? `${SNIPPET_OPEN}${w.text}${SNIPPET_CLOSE}` : w.text;
    cursor = w.end;
  }
  const prefix = from > 0 ? "…" : "";
  const suffix = to < ws.length ? "…" : "";
  return { matched: true, score, snippet: `${prefix}${snip}${suffix}` };
}
