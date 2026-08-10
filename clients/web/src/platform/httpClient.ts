import type { HttpClient, HttpResponse } from "@wa/client-core";

// fetch-backed HttpClient. The core owns retries + error mapping (otpClient).
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
