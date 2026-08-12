// WebMediaService — the browser side of the download manager. It wraps the
// framework-free DownloadManager with the web specifics: a fetch-backed
// DownloadTransport (media-svc presigned GET → bytes) and a blob-URL cache so a
// decrypted image/video/document is handed to <img>/<video>/<a> as a `blob:`
// URL, created once per object and revoked on dispose.
//
// Decryption runs on the main thread here (WebCrypto is available and an
// attachment decrypt is a one-shot AES-GCM pass, not a hot loop). If it ever
// shows up in a frame budget, it moves into the existing DB/crypto worker.

import { DownloadManager, type DownloadItem, type DownloadTransport, type MediaEnvelope } from "@wa/media-pipeline";

/** webDownloadTransport resolves a presigned GET for an object key and returns
 *  the raw ciphertext bytes. The bearer token is read fresh per call so a
 *  refresh mid-session is picked up. */
export function webDownloadTransport(apiBaseUrl: string, token: () => string): DownloadTransport {
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

export class WebMediaService {
  readonly manager: DownloadManager;
  private readonly urls = new Map<string, string>();

  constructor(transport: DownloadTransport, concurrency = 3) {
    this.manager = new DownloadManager({ transport, concurrency });
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

  /** objectUrl returns a cached `blob:` URL for an object's decrypted bytes. */
  objectUrl(objectKey: string, bytes: Uint8Array, mime: string): string {
    const cached = this.urls.get(objectKey);
    if (cached) return cached;
    const url = URL.createObjectURL(new Blob([bytes], { type: mime }));
    this.urls.set(objectKey, url);
    return url;
  }

  /** thumbnailUrl decrypts the inline preview (no network) into a `blob:` URL. */
  async thumbnailUrl(env: MediaEnvelope): Promise<string | null> {
    const key = `${env.objectKey}#thumb`;
    const cached = this.urls.get(key);
    if (cached) return cached;
    const bytes = await this.manager.thumbnail(env);
    if (!bytes) return null;
    const type = env.mime.startsWith("image/") ? env.mime : "image/jpeg";
    const url = URL.createObjectURL(new Blob([bytes], { type }));
    this.urls.set(key, url);
    return url;
  }

  /** dispose revokes every blob URL this service created. */
  dispose(): void {
    for (const url of this.urls.values()) URL.revokeObjectURL(url);
    this.urls.clear();
  }
}
