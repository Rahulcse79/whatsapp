// Push-token registration (calls-ptt-api.md §Push, PUT /v1/push/token). The VoIP
// token is what wakes a locked device to ring: apns_voip (iOS PushKit) or fcm
// (Android high-priority). Fetch-backed; bearer per call.

export type PushProvider = "fcm" | "apns" | "apns_voip" | "ntfy" | "webpush";

export function createPushRegistration(apiBaseUrl: string, token: () => string) {
  function headers(): Record<string, string> {
    return { "content-type": "application/json", authorization: `Bearer ${token()}` };
  }
  return {
    async register(provider: PushProvider, deviceToken: string): Promise<void> {
      const res = await fetch(apiBaseUrl + "/v1/push/token", {
        method: "PUT",
        headers: headers(),
        body: JSON.stringify({ provider, token: deviceToken }),
      });
      if (!res.ok) throw new Error(`push token registration failed: HTTP ${res.status}`);
    },
    async unregister(): Promise<void> {
      await fetch(apiBaseUrl + "/v1/push/token", { method: "DELETE", headers: headers() });
    },
  };
}
