// webUploadTransport drives the media-svc control-plane (create/presign/complete,
// bearer-authed) and PUTs each ciphertext part straight to object storage via its
// presigned URL, returning the stored ETag — the send-side counterpart of the
// download transport. Kept in platform (not ui) so the service layer can use it.

import type { UploadTransport } from "@wa/media-pipeline";

// refresh transparently renews an expired access token (the 10-min token may
// have lapsed since sign-in), so an upload doesn't 401 mid-flight.
export function webUploadTransport(mediaBaseUrl: string, token: () => string, refresh: () => Promise<void>): UploadTransport {
  return {
    async postJSON<T>(path: string, body: unknown): Promise<T> {
      const call = (): Promise<Response> =>
        fetch(`${mediaBaseUrl}${path}`, {
          method: "POST",
          headers: { "content-type": "application/json", authorization: `Bearer ${token()}` },
          body: JSON.stringify(body),
        });
      let res = await call();
      if (res.status === 401) {
        await refresh();
        res = await call();
      }
      if (!res.ok) throw new Error(`${path} failed: HTTP ${res.status}`);
      return (await res.json()) as T;
    },
    async putPart(url: string, bytes: Uint8Array): Promise<string> {
      const res = await fetch(url, { method: "PUT", body: bytes });
      if (!res.ok) throw new Error(`part PUT failed: HTTP ${res.status}`);
      // Object storage returns the part ETag in the header (quoted); strip quotes.
      return (res.headers.get("etag") ?? "").replace(/"/g, "");
    },
  };
}
