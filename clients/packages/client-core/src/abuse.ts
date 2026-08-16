// On-device anti-abuse heuristics (T10.03): phishing-link analysis and a spam /
// scam score. These run entirely on the client over decrypted content — the
// server never sees message text (E2EE), so detection has to live here. The
// server side only files metadata reports and rate-limits abuse. Pure + framework
// -free.

export type LinkRisk = "safe" | "caution" | "danger";

export interface LinkVerdict {
  risk: LinkRisk;
  reasons: string[];
}

const SHORTENERS = new Set([
  "bit.ly", "tinyurl.com", "t.co", "goo.gl", "ow.ly", "is.gd", "buff.ly", "rb.gy", "cutt.ly", "shorturl.at", "rebrand.ly",
]);
// TLDs disproportionately used for abuse (a coarse signal, not a blocklist).
const RISKY_TLDS = new Set(["zip", "mov", "xyz", "top", "country", "gq", "tk", "ml", "cf", "ga", "work", "click", "link"]);

function rank(r: LinkRisk): number {
  return r === "danger" ? 2 : r === "caution" ? 1 : 0;
}

/** registrableDomain returns the last two labels (a coarse eTLD+1) for comparing
 *  whether two hosts are the "same site". */
function registrableDomain(host: string): string {
  const labels = host.toLowerCase().split(".");
  return labels.slice(-2).join(".");
}

/** hostOfText pulls a hostname out of link display text when it looks like a URL
 *  or bare domain (used to catch text/href mismatch phishing). */
export function hostOfText(text: string): string | null {
  const t = text.trim();
  try {
    return new URL(/^https?:\/\//i.test(t) ? t : "https://" + t).hostname.toLowerCase();
  } catch {
    return null;
  }
}

/** analyzeLink flags a URL as safe / caution / danger with human reasons. */
export function analyzeLink(link: { href: string; text?: string }): LinkVerdict {
  const reasons: string[] = [];
  let risk: LinkRisk = "safe";
  const bump = (r: LinkRisk, why: string): void => {
    reasons.push(why);
    if (rank(r) > rank(risk)) risk = r;
  };

  let u: URL;
  try {
    u = new URL(link.href);
  } catch {
    return { risk: "caution", reasons: ["This link is malformed"] };
  }
  const host = u.hostname.toLowerCase();

  if (host.includes("xn--")) bump("danger", "Uses look-alike (punycode) characters");
  if (u.username || u.password) bump("danger", "Hides a username before the real address");
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(host)) bump("caution", "Points at a raw IP address");
  if (SHORTENERS.has(host)) bump("caution", "Shortened link hides its destination");

  const labels = host.split(".");
  const tld = labels[labels.length - 1] ?? "";
  if (RISKY_TLDS.has(tld)) bump("caution", `Unusual .${tld} domain`);
  if (labels.length > 4) bump("caution", "Suspiciously many sub-domains");

  if (link.text) {
    const textHost = hostOfText(link.text);
    if (textHost && registrableDomain(textHost) !== registrableDomain(host)) {
      bump("danger", `Text says ${textHost} but the link goes to ${host}`);
    }
  }
  return { risk, reasons };
}

const URL_RE = /\bhttps?:\/\/[^\s<>"')]+/gi;

/** extractUrls pulls http(s) URLs out of a message body. */
export function extractUrls(text: string): string[] {
  return text.match(URL_RE) ?? [];
}

/** analyzeMessageLinks returns the worst verdict across all links in a message. */
export function analyzeMessageLinks(text: string): LinkVerdict {
  let worst: LinkVerdict = { risk: "safe", reasons: [] };
  for (const href of extractUrls(text)) {
    const v = analyzeLink({ href });
    if (rank(v.risk) > rank(worst.risk)) worst = v;
  }
  return worst;
}

// ── spam / scam scoring ──────────────────────────────────────────────────────

export type SpamLevel = "none" | "low" | "high";

export interface SpamVerdict {
  level: SpamLevel;
  reasons: string[];
}

const SCAM_RE =
  /\b(free|prize|winner|congratulations|bitcoin|crypto|investment|urgent|verify your account|gift card|wire transfer|western union|claim (your|now)|act now|limited time|100% free|you'?ve won)\b/i;

function capsRatio(text: string): number {
  const letters = text.replace(/[^a-z]/gi, "");
  if (letters.length === 0) return 0;
  const upper = letters.replace(/[^A-Z]/g, "").length;
  return upper / letters.length;
}

/** scoreMessage rates a message for spam/scam risk from on-device signals: an
 *  unknown first-time sender, scam wording, a risky link, or shouting. */
export function scoreMessage(m: { text: string; fromContact: boolean; isFirstMessage: boolean; hasRiskyLink?: boolean }): SpamVerdict {
  let score = 0;
  const reasons: string[] = [];
  const add = (n: number, why: string): void => {
    score += n;
    reasons.push(why);
  };

  if (!m.fromContact && m.isFirstMessage) add(1, "First message from someone not in your contacts");
  if (SCAM_RE.test(m.text)) add(2, "Contains wording common in scams");
  if (m.hasRiskyLink) add(2, "Contains a suspicious link");
  if (m.text.length > 12 && capsRatio(m.text) > 0.6) add(1, "Written mostly in capital letters");

  const level: SpamLevel = score >= 3 ? "high" : score >= 1 ? "low" : "none";
  return { level, reasons };
}
