import { router, useLocalSearchParams } from "expo-router";
import { useState } from "react";
import { ActivityIndicator, Platform, Pressable, StyleSheet, Text, TextInput, View } from "react-native";
import type { VerifiedSession } from "@wa/client-core";
import { useServices } from "../../src/ui/ServicesContext";
import { messageOf } from "../../src/ui/errors";

function devicePlatform(): "ios" | "android" | "web" {
  return Platform.OS === "ios" ? "ios" : "android";
}

export default function Verify() {
  const { services, setAuthed } = useServices();
  const params = useLocalSearchParams<{ challengeId?: string; phone?: string }>();
  const challengeId = String(params.challengeId ?? "");
  const phone = String(params.phone ?? "");

  const [code, setCode] = useState("");
  const [pin, setPin] = useState("");
  const [needsPin, setNeedsPin] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function finish(s: VerifiedSession): Promise<void> {
    if (s.requiresPin) {
      setNeedsPin(true);
      return;
    }
    await services.completeLogin(s);
    setAuthed(true);
    router.replace("/chats");
  }

  async function onVerify(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      const s = needsPin
        ? await services.otp.verifyPin(challengeId, pin)
        : await services.otp.verifyOtp(challengeId, code, { name: "WhatsApp V2 Mobile", platform: devicePlatform() });
      await finish(s);
    } catch (e) {
      setError(messageOf(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <View style={styles.container}>
      <Text style={styles.title}>{needsPin ? "Enter your PIN" : "Enter the code"}</Text>
      <Text style={styles.subtitle}>{needsPin ? "This device needs your 2-step PIN." : `Sent to ${phone}`}</Text>
      {needsPin ? (
        <TextInput
          style={styles.input}
          value={pin}
          onChangeText={setPin}
          placeholder="••••••"
          keyboardType="number-pad"
          secureTextEntry
          autoFocus
          editable={!busy}
        />
      ) : (
        <TextInput
          style={styles.input}
          value={code}
          onChangeText={setCode}
          placeholder="123456"
          keyboardType="number-pad"
          autoFocus
          editable={!busy}
        />
      )}
      {error ? <Text style={styles.error}>{error}</Text> : null}
      <Pressable style={[styles.button, busy && styles.buttonDisabled]} onPress={onVerify} disabled={busy}>
        {busy ? <ActivityIndicator color="#fff" /> : <Text style={styles.buttonText}>Verify</Text>}
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, gap: 12, justifyContent: "center" },
  title: { fontSize: 24, fontWeight: "600" },
  subtitle: { fontSize: 15, color: "#666", marginBottom: 8 },
  input: { borderWidth: 1, borderColor: "#ccc", borderRadius: 10, padding: 14, fontSize: 22, letterSpacing: 4 },
  error: { color: "#c0362c" },
  button: { backgroundColor: "#128C7E", borderRadius: 10, padding: 16, alignItems: "center", marginTop: 8 },
  buttonDisabled: { opacity: 0.6 },
  buttonText: { color: "#fff", fontSize: 16, fontWeight: "600" },
});
