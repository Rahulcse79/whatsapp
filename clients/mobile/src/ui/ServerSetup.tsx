import { useState } from "react";
import { ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View } from "react-native";
import { deriveConfig } from "../services/serverConfig";

type TestState = null | "testing" | "ok" | "fail";

/**
 * ServerSetup lets the user point the app at their backend by typing its address
 * (e.g. a laptop's LAN IP), test that it's reachable, and save it. Shown on
 * first run and whenever the user reopens it from the sign-in screen.
 */
export function ServerSetup({
  initial,
  onSaved,
  onCancel,
}: {
  initial?: string;
  onSaved: (raw: string) => void;
  onCancel?: () => void;
}) {
  const [url, setUrl] = useState(initial ?? "");
  const [test, setTest] = useState<TestState>(null);
  const [detail, setDetail] = useState("");

  const trimmed = url.trim();
  const preview = trimmed ? deriveConfig(trimmed) : null;

  async function runTest(): Promise<void> {
    if (!preview) return;
    setTest("testing");
    setDetail("");
    try {
      const ctrl = new AbortController();
      const timer = setTimeout(() => ctrl.abort(), 5000);
      const res = await fetch(`${preview.apiBaseUrl}/readyz`, { signal: ctrl.signal });
      clearTimeout(timer);
      if (res.ok) {
        setTest("ok");
        setDetail(`Reached ${preview.apiBaseUrl}`);
      } else {
        setTest("fail");
        setDetail(`Server answered with ${res.status} — is it the right port?`);
      }
    } catch {
      setTest("fail");
      setDetail("Couldn't reach it. Check the IP, that both devices are on the same Wi-Fi, and that ./start.sh is running.");
    }
  }

  return (
    <View style={styles.wrap}>
      <Text style={styles.title}>Connect to your server</Text>
      <Text style={styles.hint}>
        Enter your computer's address on the same Wi-Fi — just the IP is enough.
      </Text>

      <TextInput
        style={styles.input}
        value={url}
        onChangeText={(t) => {
          setUrl(t);
          setTest(null);
        }}
        placeholder="192.168.1.5   (or http://host:8080)"
        placeholderTextColor="#9aa0a6"
        autoCapitalize="none"
        autoCorrect={false}
        keyboardType="url"
        inputMode="url"
      />

      {preview && (
        <Text style={styles.derived}>
          API {preview.apiBaseUrl}
          {"\n"}WS  {preview.wsUrl}
        </Text>
      )}

      <Pressable
        style={[styles.btn, styles.secondary]}
        onPress={runTest}
        disabled={!preview || test === "testing"}
      >
        {test === "testing" ? <ActivityIndicator /> : <Text style={styles.secondaryText}>Test connection</Text>}
      </Pressable>

      {test === "ok" && <Text style={styles.ok}>✓ {detail}</Text>}
      {test === "fail" && <Text style={styles.fail}>✗ {detail}</Text>}

      <Pressable
        style={[styles.btn, styles.primary, !trimmed && styles.disabled]}
        onPress={() => trimmed && onSaved(trimmed)}
        disabled={!trimmed}
      >
        <Text style={styles.primaryText}>Save &amp; continue</Text>
      </Pressable>

      {onCancel && (
        <Pressable onPress={onCancel} style={styles.cancelBtn}>
          <Text style={styles.cancel}>Cancel</Text>
        </Pressable>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { flex: 1, justifyContent: "center", padding: 24, gap: 12 },
  title: { fontSize: 22, fontWeight: "700", textAlign: "center" },
  hint: { fontSize: 14, color: "#5f6368", textAlign: "center", marginBottom: 8 },
  input: {
    borderWidth: 1,
    borderColor: "#c4c7c5",
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 12,
    fontSize: 16,
  },
  derived: { fontSize: 12, color: "#5f6368", fontFamily: "Courier" },
  btn: { borderRadius: 10, paddingVertical: 13, alignItems: "center", marginTop: 4 },
  primary: { backgroundColor: "#0a7cff" },
  primaryText: { color: "#fff", fontSize: 16, fontWeight: "600" },
  secondary: { backgroundColor: "#eef1f4" },
  secondaryText: { color: "#1a73e8", fontSize: 15, fontWeight: "600" },
  disabled: { opacity: 0.5 },
  ok: { color: "#137333", fontSize: 14 },
  fail: { color: "#c5221f", fontSize: 14 },
  cancelBtn: { alignItems: "center", paddingVertical: 8 },
  cancel: { color: "#5f6368", fontSize: 15 },
});
