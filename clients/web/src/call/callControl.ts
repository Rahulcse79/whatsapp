// CallControl REST adapter (calls-ptt-api.md §Calls). Fetch-backed; the bearer
// token is read fresh per call so a mid-session refresh is picked up.

import type { CallControl, CreateResult } from "@wa/call-engine";

export function createCallControl(apiBaseUrl: string, token: () => string): CallControl {
  async function post<T>(path: string): Promise<T> {
    const res = await fetch(apiBaseUrl + path, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${token()}` },
    });
    if (!res.ok) throw new Error(`${path} failed: HTTP ${res.status}`);
    return (await res.json()) as T;
  }

  return {
    async create(peerId, kind): Promise<CreateResult> {
      const res = await fetch(apiBaseUrl + "/v1/calls", {
        method: "POST",
        headers: { "content-type": "application/json", authorization: `Bearer ${token()}` },
        body: JSON.stringify({ callee_ids: [peerId], kind }),
      });
      if (!res.ok) throw new Error(`create call failed: HTTP ${res.status}`);
      const body = (await res.json()) as { room_id: string; ring_id: string; join_token: string };
      return { roomId: body.room_id, ringId: body.ring_id, joinToken: body.join_token };
    },
    async answer(ringId): Promise<string> {
      const body = await post<{ join_token: string }>(`/v1/calls/${encodeURIComponent(ringId)}/answer`);
      return body.join_token;
    },
    async decline(ringId): Promise<void> {
      await post<unknown>(`/v1/calls/${encodeURIComponent(ringId)}/decline`);
    },
    async rejoin(roomId): Promise<string> {
      const body = await post<{ join_token: string }>(`/v1/calls/${encodeURIComponent(roomId)}/rejoin`);
      return body.join_token;
    },
  };
}
