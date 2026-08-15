// WhatsApp-style lightweight rich-text formatting (T6.01). Pure tokenizer — the
// clients render the tokens to their own UI (React nodes on web, RN <Text> on
// mobile). Supported inline markers: *bold*, _italic_, ~strike~, `mono`, fenced
// ```code blocks```, plus autolinked http(s) URLs. Inline spans are flat (no
// nesting) which matches the common case and keeps the parser predictable.

export type RichToken =
  | { t: "text"; v: string }
  | { t: "b"; v: string }
  | { t: "i"; v: string }
  | { t: "s"; v: string }
  | { t: "code"; v: string }
  | { t: "pre"; v: string }
  | { t: "link"; v: string };

const INLINE: { re: RegExp; t: "b" | "i" | "s" | "code" }[] = [
  { re: /\*([^*\n]+)\*/, t: "b" },
  { re: /_([^_\n]+)_/, t: "i" },
  { re: /~([^~\n]+)~/, t: "s" },
  { re: /`([^`\n]+)`/, t: "code" },
];

// Autolink http(s) URLs. Trailing punctuation is left out of the link.
const URL_RE = /https?:\/\/[^\s<]+[^\s<.,;:!?)\]}'"]/g;

/** tokenizeRich turns a plaintext message body into rich-text tokens. A body
 *  with no markers/URLs yields a single text token (the common case). */
export function tokenizeRich(input: string): RichToken[] {
  const out: RichToken[] = [];
  // Fenced code blocks first: odd split segments are inside ``` ... ```.
  const parts = input.split(/```\n?([\s\S]*?)```/);
  parts.forEach((part, i) => {
    if (i % 2 === 1) {
      out.push({ t: "pre", v: part.replace(/\n$/, "") });
    } else if (part) {
      tokenizeInline(part, out);
    }
  });
  return out;
}

function tokenizeInline(text: string, out: RichToken[]): void {
  let rest = text;
  while (rest.length > 0) {
    let bestIdx = Infinity;
    let bestMatch: RegExpExecArray | null = null;
    let bestT: "b" | "i" | "s" | "code" = "b";
    for (const m of INLINE) {
      const mm = m.re.exec(rest);
      if (mm && mm.index < bestIdx) {
        bestIdx = mm.index;
        bestMatch = mm;
        bestT = m.t;
      }
    }
    if (!bestMatch) {
      pushWithLinks(rest, out);
      return;
    }
    if (bestIdx > 0) pushWithLinks(rest.slice(0, bestIdx), out);
    out.push({ t: bestT, v: bestMatch[1] ?? "" });
    rest = rest.slice(bestIdx + bestMatch[0].length);
  }
}

function pushWithLinks(text: string, out: RichToken[]): void {
  let last = 0;
  for (const m of text.matchAll(URL_RE)) {
    const idx = m.index ?? 0;
    if (idx > last) out.push({ t: "text", v: text.slice(last, idx) });
    out.push({ t: "link", v: m[0] });
    last = idx + m[0].length;
  }
  if (last < text.length) out.push({ t: "text", v: text.slice(last) });
}
