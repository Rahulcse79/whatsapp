import { router, useFocusEffect } from "expo-router";
import { useCallback, useState } from "react";
import { ActivityIndicator, FlatList, Modal, Pressable, StyleSheet, Text, TextInput, View } from "react-native";
import { isValidPhone, type ChatSummary } from "@wa/client-core";
import { useServices } from "../../src/ui/ServicesContext";

export default function Chats() {
  const { services } = useServices();
  const [items, setItems] = useState<ChatSummary[]>([]);
  const [composing, setComposing] = useState(false);
  const [phone, setPhone] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    services.messages
      .conversations()
      .then(setItems)
      .catch(() => {
        /* transient read error; next focus retries */
      });
  }, [services]);

  useFocusEffect(
    useCallback(() => {
      load();
    }, [load]),
  );

  async function startChat(): Promise<void> {
    const trimmed = phone.trim();
    if (!isValidPhone(trimmed)) {
      setError("Enter a number in international format, e.g. +14155550123.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const conversationId = await services.startDirectChat(trimmed);
      setComposing(false);
      setPhone("");
      router.push(`/thread/${conversationId}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Couldn't start the conversation.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <View style={styles.container}>
      <Pressable style={styles.searchBar} onPress={() => router.push("/search")} accessibilityLabel="Search messages">
        <Text style={styles.searchText}>🔍  Search messages</Text>
      </Pressable>
      <FlatList
        data={items}
        keyExtractor={(c) => c.conversationId}
        renderItem={({ item }) => (
          <Pressable style={styles.row} onPress={() => router.push(`/thread/${item.conversationId}`)}>
            <Text style={styles.title} numberOfLines={1}>
              {item.title}
            </Text>
            <Text style={styles.preview} numberOfLines={1}>
              {item.lastPreview || "No messages yet"}
            </Text>
          </Pressable>
        )}
        ListEmptyComponent={<Text style={styles.empty}>No conversations yet. Tap ＋ to start one.</Text>}
        contentContainerStyle={items.length === 0 ? styles.emptyContainer : undefined}
      />
      <Pressable style={styles.fab} onPress={() => setComposing(true)} accessibilityLabel="New conversation">
        <Text style={styles.fabText}>＋</Text>
      </Pressable>

      <Modal visible={composing} animationType="slide" transparent onRequestClose={() => setComposing(false)}>
        <View style={styles.sheetBackdrop}>
          <View style={styles.sheet}>
            <Text style={styles.sheetTitle}>New chat</Text>
            <Text style={styles.sheetHint}>Enter the phone number of someone who has an account.</Text>
            <TextInput
              style={styles.input}
              value={phone}
              onChangeText={(t) => {
                setPhone(t);
                setError(null);
              }}
              placeholder="+14155550123"
              keyboardType="phone-pad"
              autoFocus
              editable={!busy}
            />
            {error ? <Text style={styles.error}>{error}</Text> : null}
            <View style={styles.sheetActions}>
              <Pressable style={styles.cancelBtn} onPress={() => setComposing(false)} disabled={busy}>
                <Text style={styles.cancelText}>Cancel</Text>
              </Pressable>
              <Pressable style={[styles.startBtn, busy && styles.disabled]} onPress={startChat} disabled={busy}>
                {busy ? <ActivityIndicator color="#fff" /> : <Text style={styles.startText}>Start chat</Text>}
              </Pressable>
            </View>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  searchBar: { margin: 12, paddingHorizontal: 14, paddingVertical: 10, borderRadius: 10, backgroundColor: "#f0f0f0" },
  searchText: { fontSize: 15, color: "#777" },
  row: { paddingHorizontal: 20, paddingVertical: 16, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: "#e2e2e2" },
  title: { fontSize: 17, fontWeight: "600" },
  preview: { fontSize: 14, color: "#777", marginTop: 2 },
  empty: { textAlign: "center", color: "#999", fontSize: 15 },
  emptyContainer: { flexGrow: 1, alignItems: "center", justifyContent: "center", padding: 24 },
  fab: {
    position: "absolute",
    right: 20,
    bottom: 28,
    width: 56,
    height: 56,
    borderRadius: 28,
    backgroundColor: "#128C7E",
    alignItems: "center",
    justifyContent: "center",
    elevation: 4,
  },
  fabText: { color: "#fff", fontSize: 28, lineHeight: 30 },
  sheetBackdrop: { flex: 1, justifyContent: "flex-end", backgroundColor: "rgba(0,0,0,0.4)" },
  sheet: { backgroundColor: "#fff", padding: 20, borderTopLeftRadius: 16, borderTopRightRadius: 16, gap: 10 },
  sheetTitle: { fontSize: 20, fontWeight: "700" },
  sheetHint: { fontSize: 14, color: "#666" },
  input: { borderWidth: 1, borderColor: "#ccc", borderRadius: 10, padding: 14, fontSize: 17 },
  error: { color: "#c0362c" },
  sheetActions: { flexDirection: "row", justifyContent: "flex-end", gap: 8, marginTop: 4 },
  cancelBtn: { paddingHorizontal: 18, paddingVertical: 12 },
  cancelText: { color: "#666", fontSize: 15, fontWeight: "600" },
  startBtn: { backgroundColor: "#128C7E", borderRadius: 10, paddingHorizontal: 22, paddingVertical: 12, minWidth: 120, alignItems: "center" },
  startText: { color: "#fff", fontSize: 15, fontWeight: "600" },
  disabled: { opacity: 0.6 },
});
