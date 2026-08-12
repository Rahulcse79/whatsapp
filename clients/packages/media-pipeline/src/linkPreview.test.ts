import { describe, expect, it } from "vitest";
import {
  detectFirstUrl,
  generateLinkPreview,
  isHttpUrl,
  parseLinkMetadata,
  type HtmlFetcher,
  type PreviewFetch,
} from "./linkPreview";

describe("detectFirstUrl", () => {
  it("finds the first http(s) URL in a message", () => {
    expect(detectFirstUrl("look at https://example.com/page it's neat")).toBe("https://example.com/page");
    expect(detectFirstUrl("no link here")).toBeNull();
  });
  it("trims trailing sentence punctuation", () => {
    expect(detectFirstUrl("see https://example.com.")).toBe("https://example.com");
    expect(detectFirstUrl("(https://example.com)")).toBe("https://example.com");
  });
  it("keeps a balanced paren inside the URL", () => {
    expect(detectFirstUrl("https://en.wikipedia.org/wiki/Foo_(bar)")).toBe("https://en.wikipedia.org/wiki/Foo_(bar)");
  });
  it("ignores non-http schemes", () => {
    expect(detectFirstUrl("ftp://example.com/file")).toBeNull();
    expect(isHttpUrl("ftp://example.com")).toBe(false);
    expect(isHttpUrl("https://example.com")).toBe(true);
  });
});

describe("parseLinkMetadata", () => {
  const page = "https://example.com/articles/1";

  it("prefers OpenGraph tags (attr order independent)", () => {
    const html = `
      <meta content="OG Title" property="og:title">
      <meta property="og:description" content="OG desc">
      <meta property="og:site_name" content="Example">
      <meta property="og:image" content="https://cdn.example.com/a.png">
      <title>fallback</title>`;
    expect(parseLinkMetadata(html, page)).toEqual({
      title: "OG Title",
      description: "OG desc",
      siteName: "Example",
      imageUrl: "https://cdn.example.com/a.png",
    });
  });

  it("falls back to twitter cards, then <title> and meta description", () => {
    const twitter = parseLinkMetadata(
      `<meta name="twitter:title" content="TW"><meta name="twitter:image" content="/img/x.jpg">`,
      page,
    );
    expect(twitter.title).toBe("TW");
    expect(twitter.imageUrl).toBe("https://example.com/img/x.jpg"); // resolved relative to page

    const bare = parseLinkMetadata(`<title>Just a title</title><meta name="description" content="d">`, page);
    expect(bare).toEqual({ title: "Just a title", description: "d" });
  });

  it("decodes HTML entities and collapses whitespace", () => {
    const html = `<meta property="og:title" content="Tom &amp; Jerry &#39;99   &#x2764;">`;
    expect(parseLinkMetadata(html, page).title).toBe("Tom & Jerry '99 ❤");
  });

  it("returns an empty object when there is nothing to preview", () => {
    expect(parseLinkMetadata("<p>no meta, no title</p>", page)).toEqual({});
  });
});

// A fake fetcher so the async generator is deterministic and offline.
function fakeFetcher(res: PreviewFetch | null): HtmlFetcher {
  return () => Promise.resolve(res);
}

describe("generateLinkPreview", () => {
  const html = `<meta property="og:title" content="Hello"><meta property="og:description" content="world"><meta property="og:image" content="https://cdn.example.com/i.png">`;
  const ok: PreviewFetch = { finalUrl: "https://example.com/final", contentType: "text/html; charset=utf-8", html };

  it("builds a preview from the first URL's metadata", async () => {
    const p = await generateLinkPreview("check https://example.com now", fakeFetcher(ok));
    expect(p).toEqual({ url: "https://example.com/final", title: "Hello", description: "world" });
  });

  it("embeds the image via the injected embedder (no remote URL carried)", async () => {
    let embedderSawUrl: string | undefined;
    const p = await generateLinkPreview("https://example.com", fakeFetcher(ok), {
      embedImage: (u) => {
        embedderSawUrl = u;
        return Promise.resolve("data:image/png;base64,AA==");
      },
    });
    // The embedder is fed the parsed image URL...
    expect(embedderSawUrl).toBe("https://cdn.example.com/i.png");
    // ...but the preview carries only its self-contained output, never the
    // remote host — the recipient has nothing to fetch (FR-MSG-08 privacy).
    expect(p?.image).toBe("data:image/png;base64,AA==");
    expect(JSON.stringify(p)).not.toContain("cdn.example.com");
    expect((p as unknown as Record<string, unknown>).imageUrl).toBeUndefined();
  });

  it("ignores an embedder failure (image simply absent)", async () => {
    const p = await generateLinkPreview("https://example.com", fakeFetcher(ok), {
      embedImage: () => Promise.reject(new Error("boom")),
    });
    expect(p?.title).toBe("Hello");
    expect(p?.image).toBeUndefined();
  });

  it("is best-effort: returns null instead of throwing on every failure mode", async () => {
    expect(await generateLinkPreview("no url in here", fakeFetcher(ok))).toBeNull();
    expect(await generateLinkPreview("ftp://example.com/x", fakeFetcher(ok))).toBeNull();
    expect(await generateLinkPreview("https://example.com", fakeFetcher(null))).toBeNull();
    expect(
      await generateLinkPreview("https://example.com", fakeFetcher({ ...ok, contentType: "application/json" })),
    ).toBeNull();
    expect(
      await generateLinkPreview("https://example.com", fakeFetcher({ ...ok, html: "<p>no title</p>" })),
    ).toBeNull();
    const throwing: HtmlFetcher = () => Promise.reject(new Error("network"));
    expect(await generateLinkPreview("https://example.com", throwing)).toBeNull();
  });
});
