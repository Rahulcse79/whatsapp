// Web HtmlFetcher: fetches a page on the SENDER's device so a link preview can
// be built and carried in the envelope (FR-MSG-08). Best-effort — returns null
// on any failure. NOTE: browser fetches of cross-origin pages are subject to
// CORS, so many sites will not be previewable from the web client; that is an
// accepted degradation (the mobile client, with no CORS, previews freely).

import type { HtmlFetcher } from "@wa/media-pipeline";

const TIMEOUT_MS = 5000;
const MAX_BYTES = 512 * 1024;

export const webHtmlFetcher: HtmlFetcher = async (url) => {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);
  try {
    const res = await fetch(url, { signal: controller.signal, redirect: "follow" });
    const contentType = res.headers.get("content-type") ?? "";
    if (!res.ok || !/text\/html/i.test(contentType)) return null;
    const html = (await res.text()).slice(0, MAX_BYTES);
    return { finalUrl: res.url || url, contentType, html };
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
};
