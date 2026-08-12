// RnMediaService — the React Native side of the download manager. Same shared
// DownloadManager as the web client; the platform difference is delivery: RN's
// <Image> takes a `data:` URI, so decrypted bytes become a base64 data URI
// (cached per object). Audio/video *playback* and file *saving* need native
// modules (expo-av/expo-video, expo-file-system) that aren't wired yet — they're
// injected as optional handlers so the UI is complete and those adapters drop in
// later without touching components.

import {
  DownloadManager,
  toBase64,
  type DownloadItem,
  type DownloadTransport,
  type MediaEnvelope,
} from "@wa/media-pipeline";

/** Optional native adapters, injected at construction (deferred by default). */
export interface RnMediaHandlers {
  /** Persist/share a decrypted attachment (expo-file-system / Sharing). */
  onSave?: (env: MediaEnvelope, bytes: Uint8Array) => void | Promise<void>;
  /** Play decrypted audio/video (expo-av / expo-video). */
  onPlay?: (env: MediaEnvelope, bytes: Uint8Array) => void | Promise<void>;
}

/** rnDownloadTransport resolves a presigned GET and returns the ciphertext. */
export function rnDownloadTransport(apiBaseUrl: string, token: () => string): DownloadTransport {
  return {
    async get(objectKey: string): Promise<Uint8Array> {
      const res = await fetch(`${apiBaseUrl}/v1/media/download-urls`, {
        method: "POST",
        headers: { "content-type": "application/json", authorization: `Bearer ${token()}` },
        body: JSON.stringify({ object_keys: [objectKey] }),
      });
      if (!res.ok) throw new Error(`download-urls failed: HTTP ${res.status}`);
      const body = (await res.json()) as { urls?: Array<{ key: string; url: string }> };
      const first = body.urls?.[0];
      if (!first) throw new Error("no download URL returned for object");
      const obj = await fetch(first.url);
      if (!obj.ok) throw new Error(`object fetch failed: HTTP ${obj.status}`);
      return new Uint8Array(await obj.arrayBuffer());
    },
  };
}

export class RnMediaService {
  readonly manager: DownloadManager;
  readonly handlers: RnMediaHandlers;
  private readonly uris = new Map<string, string>();

  constructor(transport: DownloadTransport, handlers: RnMediaHandlers = {}, concurrency = 3) {
    this.manager = new DownloadManager({ transport, concurrency });
    this.handlers = handlers;
  }

  request(env: MediaEnvelope): DownloadItem {
    return this.manager.request(env);
  }
  retry(objectKey: string): void {
    this.manager.retry(objectKey);
  }
  subscribe(fn: (item: DownloadItem) => void): () => void {
    return this.manager.subscribe(fn);
  }
  items(): DownloadItem[] {
    return this.manager.items();
  }

  /** dataUri returns a cached `data:` URI for an object's decrypted bytes. */
  dataUri(objectKey: string, bytes: Uint8Array, mime: string): string {
    const cached = this.uris.get(objectKey);
    if (cached) return cached;
    const uri = `data:${mime};base64,${toBase64(bytes)}`;
    this.uris.set(objectKey, uri);
    return uri;
  }

  /** thumbnailUri decrypts the inline preview into a `data:` URI (no network). */
  async thumbnailUri(env: MediaEnvelope): Promise<string | null> {
    const key = `${env.objectKey}#thumb`;
    const cached = this.uris.get(key);
    if (cached) return cached;
    const bytes = await this.manager.thumbnail(env);
    if (!bytes) return null;
    const mime = env.mime.startsWith("image/") ? env.mime : "image/jpeg";
    const uri = `data:${mime};base64,${toBase64(bytes)}`;
    this.uris.set(key, uri);
    return uri;
  }

  dispose(): void {
    this.uris.clear();
  }
}
