import { router } from "expo-router";
import { useState } from "react";
import { ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View } from "react-native";
import { isValidPhone } from "@wa/client-core";
import { useServices } from "../../src/ui/ServicesContext";
import { messageOf } from "../../src/ui/errors";

export default function Login() {
  const { services } = useServices();
  const [phone, setPhone] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(): Promise<void> {
    const trimmed = phone.trim();
    if (!isValidPhone(trimmed)) {
      setError("Enter your number in international format, e.g. +14155550123.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const ch = await services.otp.requestOtp(trimmed);
      router.push(`/verify?challengeId=${encodeURIComponent(ch.challengeId)}&phone=${encodeURIComponent(trimmed)}`);
    } catch (e) {
      setError(messageOf(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Your phone number</Text>
      <Text style={styles.subtitle}>We&apos;ll send a one-time code to confirm it&apos;s you.</Text>
      <TextInput
        style={styles.input}
        value={phone}
        onChangeText={setPhone}
        placeholder="+14155550123"
        keyboardType="phone-pad"
        autoComplete="tel"
        autoFocus
        editable={!busy}
      />
      {error ? <Text style={styles.error}>{error}</Text> : null}
      <Pressable style={[styles.button, busy && styles.buttonDisabled]} onPress={onSubmit} disabled={busy}>
        {busy ? <ActivityIndicator color="#fff" /> : <Text style={styles.buttonText}>Send code</Text>}
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, gap: 12, justifyContent: "center" },
  title: { fontSize: 24, fontWeight: "600" },
  subtitle: { fontSize: 15, color: "#666", marginBottom: 8 },
  input: { borderWidth: 1, borderColor: "#ccc", borderRadius: 10, padding: 14, fontSize: 18 },
  error: { color: "#c0362c" },
  button: { backgroundColor: "#128C7E", borderRadius: 10, padding: 16, alignItems: "center", marginTop: 8 },
  buttonDisabled: { opacity: 0.6 },
  buttonText: { color: "#fff", fontSize: 16, fontWeight: "600" },
});
