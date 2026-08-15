// CallOverlay (mobile) renders the active call in a Modal, driven by the
// CallSession phase: incoming ring (accept/decline), outgoing ring, the
// connecting/connected bar, and a brief ended notice. Hidden when idle.

import { active, type CallState } from "@wa/call-engine";
import { RTCView } from "@livekit/react-native-webrtc";
import { useEffect, useState } from "react";
import { Modal, Pressable, StyleSheet, Text, View } from "react-native";
import { useCall } from "./CallContext";

export function CallOverlay() {
  const { state, camera, screen, effect, localStreamURL, remoteStreamURL, accept, decline, hangup, toggleCamera, flipCamera, toggleScreenShare, toggleBlur } =
    useCall();
  const [showEnded, setShowEnded] = useState(false);
  const [localURL, setLocalURL] = useState<string | null>(null);
  const [remoteURL, setRemoteURL] = useState<string | null>(null);
  const isVideo = state.kind === "video";
  const inCall = state.phase === "connecting" || state.phase === "connected";

  useEffect(() => {
    if (state.phase !== "ended") return;
    setShowEnded(true);
    const t = setTimeout(() => setShowEnded(false), 2500);
    return () => clearTimeout(t);
  }, [state.phase, state.endReason]);

  // Remote/local video tracks arrive asynchronously after connect (and toggle
  // during the call), so poll the transport for their stream URLs while active.
  useEffect(() => {
    if (!inCall) {
      setLocalURL(null);
      setRemoteURL(null);
      return;
    }
    const tick = (): void => {
      setLocalURL(localStreamURL());
      setRemoteURL(remoteStreamURL());
    };
    tick();
    const h = setInterval(tick, 700);
    return () => clearInterval(h);
  }, [inCall, localStreamURL, remoteStreamURL]);

  const visible = (state.phase !== "idle" && state.phase !== "ended") || showEnded;
  if (!visible) return null;

  return (
    <Modal visible transparent animationType="fade" onRequestClose={() => void hangup()}>
      <View style={styles.backdrop}>
        <View style={styles.card}>
          {state.phase === "ended" ? (
            <Text style={styles.status}>Call ended{state.endReason ? ` · ${state.endReason}` : ""}</Text>
          ) : (
            <>
              {isVideo && remoteURL ? <RTCView streamURL={remoteURL} style={styles.remote} objectFit="cover" /> : null}
              {isVideo && camera.enabled ? (
                localURL ? (
                  <RTCView streamURL={localURL} style={styles.preview} objectFit="cover" mirror zOrder={1} />
                ) : (
                  <View style={styles.preview} />
                )
              ) : null}
              <Text style={styles.peer}>{state.peerId?.slice(0, 12) ?? "unknown"}</Text>
              <Text style={styles.status}>{statusLabel(state)}</Text>
              <View style={styles.actions}>
                {state.phase === "incoming" ? (
                  <>
                    <Pressable style={[styles.btn, styles.danger]} onPress={() => void decline()}>
                      <Text style={styles.btnText}>Decline</Text>
                    </Pressable>
                    <Pressable style={styles.btn} onPress={() => void accept()}>
                      <Text style={styles.btnText}>Accept</Text>
                    </Pressable>
                  </>
                ) : (
                  <>
                    {isVideo && inCall ? (
                      <>
                        <Pressable style={[styles.btn, styles.ghost]} onPress={() => void toggleCamera()}>
                          <Text style={styles.ghostText}>{camera.enabled ? "Camera off" : "Camera on"}</Text>
                        </Pressable>
                        {camera.enabled ? (
                          <>
                            <Pressable style={[styles.btn, styles.ghost]} onPress={() => void flipCamera()}>
                              <Text style={styles.ghostText}>Flip</Text>
                            </Pressable>
                            <Pressable style={[styles.btn, styles.ghost]} onPress={() => void toggleBlur()}>
                              <Text style={styles.ghostText}>{effect.effect === "blur" ? "Unblur" : "Blur"}</Text>
                            </Pressable>
                          </>
                        ) : null}
                      </>
                    ) : null}
                    {inCall ? (
                      <Pressable style={[styles.btn, styles.ghost]} onPress={() => void toggleScreenShare()}>
                        <Text style={styles.ghostText}>{screen.sharing ? "Stop share" : "Share screen"}</Text>
                      </Pressable>
                    ) : null}
                    <Pressable style={[styles.btn, styles.danger]} onPress={() => void hangup()}>
                      <Text style={styles.btnText}>{active(state) ? "End" : "Cancel"}</Text>
                    </Pressable>
                  </>
                )}
              </View>
            </>
          )}
        </View>
      </View>
    </Modal>
  );
}

function statusLabel(state: CallState): string {
  switch (state.phase) {
    case "outgoing":
      return "Ringing…";
    case "incoming":
      return `Incoming ${state.kind ?? "voice"} call`;
    case "connecting":
      return "Connecting…";
    case "connected":
      return "Connected";
    default:
      return "";
  }
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: "rgba(0,0,0,0.5)", alignItems: "center", justifyContent: "center" },
  card: { backgroundColor: "#fff", borderRadius: 16, padding: 28, minWidth: 260, alignItems: "center" },
  peer: { fontSize: 18, fontFamily: "monospace", marginBottom: 6 },
  status: { color: "#777", marginBottom: 20 },
  actions: { flexDirection: "row", gap: 12, flexWrap: "wrap", justifyContent: "center" },
  btn: { backgroundColor: "#128C7E", paddingHorizontal: 20, paddingVertical: 10, borderRadius: 24 },
  danger: { backgroundColor: "#e5484d" },
  ghost: { backgroundColor: "#eee" },
  ghostText: { color: "#333", fontSize: 15 },
  btnText: { color: "#fff", fontSize: 15 },
  // Remote camera (the person you're talking to) fills the top of the card;
  // the local self-view is a small mirrored tile above it.
  remote: { width: 260, height: 320, borderRadius: 12, backgroundColor: "#000", marginBottom: 12 },
  preview: { width: 120, height: 160, borderRadius: 10, backgroundColor: "#222", marginBottom: 14 },
});
