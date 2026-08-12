// Sender-side link previews (FR-MSG-08). The preview is generated on the
// SENDER's device and carried inside the E2EE message envelope; the recipient
// renders it WITHOUT any network fetch of its own. That preserves privacy — the
// recipient's IP is never exposed to the linked site — and keeps the server
// blind to which URLs users share (there is no server-side unfurling). This
// module is framework-free and pure except for the injected fetch/embed ports;
// unit-tested in Node.

/** A link preview carried in the sealed message body. Self-contained: any image
 *  is embedded as a data: URI, so the recipient never fetches the source. */
export interface LinkPreview {
  /** The (final, post-redirect) URL previewed. */
  url: string;
  /** Page title — always present (a preview with no title is not produced). */
  title: string;
  /** Short description / summary. */
  description?: string;
  /** Site name (og:site_name), e.g. "GitHub". */
  siteName?: string;
  /** Self-contained preview image as a data: URI (never a remote URL). */
  image?: string;
}

/** Metadata parsed out of a page's HTML (internal; imageUrl resolved absolute). */
export interface LinkMetadata {
  title?: string;
  description?: string;
  imageUrl?: string;
  siteName?: string;
}

/** Result of fetching a page for preview. */
export interface PreviewFetch {
  /** The URL after redirects — what the preview is attributed to. */
  finalUrl: string;
  /** Response content type; only text/html is parsed. */
  contentType: string;
  /** The response body (HTML). The adapter should already have size-capped it. */
  html: string;
}

/**
 * HtmlFetcher fetches a URL for preview. Platform adapters implement it over
 * fetch and MUST enforce the network-side guards the pure core cannot: block
 * private/loopback/link-local hosts (SSRF), cap the body size, and time out.
 * Returns null when the URL can't be previewed.
 */
export type HtmlFetcher = (url: string) => Promise<PreviewFetch | null>;

/**
 * ImageEmbedder turns a preview-image URL into a self-contained data: URI
 * (fetch + downscale + encode). Optional — when absent, previews carry no image.
 */
export type ImageEmbedder = (imageUrl: string) => Promise<string | null>;

export interface GeneratePreviewOptions {
  embedImage?: ImageEmbedder;
}

const URL_RE = /\bhttps?:\/\/[^\s<>"']+/i;

/** detectFirstUrl returns the first http(s) URL in text, trimming trailing
 *  sentence punctuation and a single unbalanced closing paren. */
export function detectFirstUrl(text: string): string | null {
  const first = text.match(URL_RE)?.[0];
  if (!first) return null;
  let url = first.replace(/[.,;:!?]+$/, "");
  if (url.endsWith(")") && !url.includes("(")) url = url.slice(0, -1);
  return url;
}

/** isHttpUrl guards the scheme — only http/https are previewable. */
export function isHttpUrl(url: string): boolean {
  return /^https?:\/\//i.test(url);
}

const META_RE = /<meta\b[^>]*>/gi;
const ATTR_RE = /([\w:-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')/g;
const TITLE_RE = /<title[^>]*>([\s\S]*?)<\/title>/i;

function attrs(tag: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const m of tag.matchAll(ATTR_RE)) {
    const name = m[1];
    if (!name) continue;
    out[name.toLowerCase()] = decodeEntities(m[2] ?? m[3] ?? "");
  }
  return out;
}

/**
 * parseLinkMetadata extracts preview metadata from HTML using OpenGraph, then
 * Twitter-card, then plain <title> / <meta name="description"> fallbacks.
 * Relative image URLs are resolved against pageUrl. Pure — no DOM (regex over
 * <meta> tags, so it runs in a worker / RN / Node alike).
 */
export function parseLinkMetadata(html: string, pageUrl: string): LinkMetadata {
  const tags: Record<string, string> = {};
  for (const m of html.matchAll(META_RE)) {
    const a = attrs(m[0]);
    const key = (a.property ?? a.name ?? "").toLowerCase();
    const content = a.content;
    if (!key || content === undefined) continue;
    if (!(key in tags)) tags[key] = content; // first occurrence wins
  }

  const title = tags["og:title"] ?? tags["twitter:title"] ?? titleTag(html);
  const description = tags["og:description"] ?? tags["twitter:description"] ?? tags["description"];
  const siteName = tags["og:site_name"];
  const rawImage = tags["og:image"] ?? tags["og:image:url"] ?? tags["twitter:image"] ?? tags["twitter:image:src"];
  const imageUrl = rawImage ? absoluteUrl(rawImage, pageUrl) : undefined;

  const meta: LinkMetadata = {};
  if (title) meta.title = collapse(title);
  if (description) meta.description = collapse(description);
  if (siteName) meta.siteName = collapse(siteName);
  if (imageUrl) meta.imageUrl = imageUrl;
  return meta;
}

/**
 * generateLinkPreview detects the first URL in text, fetches it via the injected
 * fetcher, and builds a preview from the parsed metadata. Best-effort by
 * contract: ANY failure (no URL, non-http scheme, fetch error/null, non-HTML,
 * no title) yields null so a preview never blocks or breaks the send path.
 */
export async function generateLinkPreview(
  text: string,
  fetchHtml: HtmlFetcher,
  opts: GeneratePreviewOptions = {},
): Promise<LinkPreview | null> {
  const url = detectFirstUrl(text);
  if (!url || !isHttpUrl(url)) return null;
  try {
    const res = await fetchHtml(url);
    if (!res || !/text\/html/i.test(res.contentType)) return null;
    const meta = parseLinkMetadata(res.html, res.finalUrl || url);
    if (!meta.title) return null; // a preview with no title isn't worth showing
    const preview: LinkPreview = { url: res.finalUrl || url, title: meta.title };
    if (meta.description) preview.description = meta.description;
    if (meta.siteName) preview.siteName = meta.siteName;
    if (opts.embedImage && meta.imageUrl) {
      const image = await opts.embedImage(meta.imageUrl).catch(() => null);
      if (image) preview.image = image;
    }
    return preview;
  } catch {
    return null;
  }
}

function titleTag(html: string): string | undefined {
  const c = TITLE_RE.exec(html)?.[1];
  return c === undefined ? undefined : decodeEntities(c);
}

function absoluteUrl(ref: string, base: string): string | undefined {
  try {
    return new URL(ref, base).toString();
  } catch {
    return undefined;
  }
}

function collapse(s: string): string {
  return s.replace(/\s+/g, " ").trim();
}

const NAMED_ENTITIES: Record<string, string> = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'" };

function decodeEntities(s: string): string {
  return s.replace(/&(#x?[0-9a-f]+|[a-z]+);/gi, (full: string, code: string) => {
    const key = code.toLowerCase();
    const named = NAMED_ENTITIES[key];
    if (named !== undefined) return named;
    if (key.charAt(0) === "#") {
      const cp = key.charAt(1) === "x" ? parseInt(key.slice(2), 16) : parseInt(key.slice(1), 10);
      if (Number.isFinite(cp) && cp > 0 && cp <= 0x10ffff) return String.fromCodePoint(cp);
    }
    return full;
  });
}
