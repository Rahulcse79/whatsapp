// VoIP push wiring (mobile-app-architecture.md §3). iOS: react-native-voip-push-
// notification surfaces the PushKit token + the wake payload; the handler MUST
// report to CallKit immediately (RingBridge.incoming does that first). Android:
// a high-priority FCM data message wakes a headless task that calls the same
// handler. Both native modules need config plugins, so neither is imported here —
// the APIs are declared structurally and injected at app wiring time.

import type { IncomingCall } from "@wa/call-engine";
import type { PushProvider } from "./pushRegistration";

/** react-native-voip-push-notification subset (iOS PushKit). */
export interface VoipPushLike {
  addEventListener(type: "register" | "notification" | "didLoadWithEvents", handler: (payload: unknown) => void): void;
  registerVoipToken(): void;
  onVoipNotificationCompleted?(uuid: string): void;
}

/** parseCallPush extracts the ring info from a VoIP push payload. ring_id is
 *  required (the capability); room/caller/kind enrich the ring UI when the push
 *  carries them (the caller is otherwise fetched after the immediate report). */
export function parseCallPush(payload: unknown): IncomingCall | null {
  if (typeof payload !== "object" || payload === null) return null;
  const p = payload as Record<string, unknown>;
  const ringId = str(p.ring_id) ?? str(p.ringId);
  if (!ringId) return null;
  const kind = p.kind === "video" ? "video" : "voice";
  return {
    ringId,
    roomId: str(p.room_id) ?? str(p.roomId) ?? "",
    peerId: str(p.caller_id) ?? str(p.callerId) ?? "unknown",
    kind,
  };
}

/** installVoipPush registers the PushKit token and routes incoming call pushes to
 *  onIncoming (which must synchronously report to CallKit). Returns nothing; the
 *  iOS token arrives asynchronously via the "register" event. */
export function installVoipPush(
  voip: VoipPushLike,
  registerToken: (provider: PushProvider, token: string) => Promise<void>,
  onIncoming: (call: IncomingCall) => void,
): void {
  voip.addEventListener("register", (payload) => {
    const token = typeof payload === "string" ? payload : str((payload as Record<string, unknown>)?.token);
    if (token) void registerToken("apns_voip", token);
  });
  voip.addEventListener("notification", (payload) => {
    const call = parseCallPush(payload);
    if (call) onIncoming(call); // RingBridge.incoming → CallKit first, then session
  });
  voip.addEventListener("didLoadWithEvents", (events) => {
    // Pushes delivered while the JS engine was cold are replayed here.
    for (const ev of asArray(events)) {
      const call = parseCallPush((ev as Record<string, unknown>)?.data ?? ev);
      if (call) onIncoming(call);
    }
  });
  voip.registerVoipToken();
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v.length > 0 ? v : undefined;
}
function asArray(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}
