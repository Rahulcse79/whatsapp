import type { HttpClient, HttpResponse } from "../core/ports";

// fetch-backed HttpClient. Kept trivial: the core owns retries/backoff and error
// mapping (otpClient), this just carries JSON over the wire.
export function createHttpClient(baseUrl: string): HttpClient {
  return {
    async post(path: string, body: unknown, headers?: Record<string, string>): Promise<HttpResponse> {
      const res = await fetch(baseUrl + path, {
        method: "POST",
        headers: { "content-type": "application/json", ...(headers ?? {}) },
        body: JSON.stringify(body),
      });
      return { status: res.status, json: () => res.json() as Promise<unknown> };
    },
  };
}
