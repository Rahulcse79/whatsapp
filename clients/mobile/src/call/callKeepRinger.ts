// NativeRinger over react-native-callkeep — the unified CallKit (iOS) /
// ConnectionService (Android) integration for lock-screen ringing
// (mobile-app-architecture.md §3). callkeep is a native module with config
// plugins, so it is NOT imported here; the API subset we use is declared
// structurally and the real module is injected at app wiring time (added with
// the dep). The call UUID is our server ringId (a UUIDv7).

import type { IncomingCall, NativeRinger } from "@wa/call-engine";

/** The react-native-callkeep API surface this adapter uses. */
export interface CallKeepLike {
  setup(options: unknown): Promise<void>;
  displayIncomingCall(uuid: string, handle: string, localizedCallerName?: string, handleType?: string, hasVideo?: boolean): void;
  startCall(uuid: string, handle: string, contactIdentifier?: string, handleType?: string, hasVideo?: boolean): void;
  setCurrentCallActive(uuid: string): void;
  endCall(uuid: string): void;
  addEventListener(type: string, handler: (args: { callUUID: string }) => void): void;
  removeEventListener(type: string): void;
}

/** setupCallKeep configures CallKit/ConnectionService. Call once at startup.
 *  Self-managed on Android so ConnectionService raises the full-screen intent. */
export function setupCallKeep(callKeep: CallKeepLike, appName = "WhatsApp V2"): Promise<void> {
  return callKeep.setup({
    ios: { appName },
    android: {
      alertTitle: "Permissions required",
      alertDescription: "This app needs to display call notifications",
      cancelButton: "Cancel",
      okButton: "OK",
      foregroundService: {
        channelId: "com.wa.call",
        channelName: "Calls",
        notificationTitle: "Call in progress",
      },
      selfManaged: true, // ConnectionService full-screen intent (locked ringing)
    },
  });
}

/** createCallKeepRinger maps the NativeRinger port onto callkeep. */
export function createCallKeepRinger(callKeep: CallKeepLike): NativeRinger {
  return {
    reportIncoming(call: IncomingCall): void {
      callKeep.displayIncomingCall(call.ringId, call.peerId, call.peerId, "generic", call.kind === "video");
    },
    reportOutgoing(call: IncomingCall): void {
      callKeep.startCall(call.ringId, call.peerId, call.peerId, "generic", call.kind === "video");
    },
    reportConnected(ringId: string): void {
      callKeep.setCurrentCallActive(ringId);
    },
    reportEnded(ringId: string): void {
      callKeep.endCall(ringId);
    },
    onAnswer(cb: (ringId: string) => void): void {
      callKeep.addEventListener("answerCall", ({ callUUID }) => cb(callUUID));
    },
    onEnd(cb: (ringId: string) => void): void {
      callKeep.addEventListener("endCall", ({ callUUID }) => cb(callUUID));
    },
  };
}
