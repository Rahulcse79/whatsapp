// CallOverlay (mobile) renders the active call in a Modal, driven by the
// CallSession phase: incoming ring (accept/decline), outgoing ring, the
// connecting/connected bar, and a brief ended notice. Hidden when idle.

import { active, type CallState } from "@wa/call-engine";
import { useEffect, useState } from "react";
import { Modal, Pressable, StyleSheet, Text, View } from "react-native";
import { useCall } from "./CallContext";

export function CallOverlay() {
  const { state, accept, decline, hangup } = useCall();
  const [showEnded, setShowEnded] = useState(false);

  useEffect(() => {
    if (state.phase !== "ended") return;
    setShowEnded(true);
    const t = setTimeout(() => setShowEnded(false), 2500);
    return () => clearTimeout(t);
  }, [state.phase, state.endReason]);

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
                  <Pressable style={[styles.btn, styles.danger]} onPress={() => void hangup()}>
                    <Text style={styles.btnText}>{active(state) ? "End" : "Cancel"}</Text>
                  </Pressable>
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
  actions: { flexDirection: "row", gap: 12 },
  btn: { backgroundColor: "#128C7E", paddingHorizontal: 20, paddingVertical: 10, borderRadius: 24 },
  danger: { backgroundColor: "#e5484d" },
  btnText: { color: "#fff", fontSize: 15 },
});
