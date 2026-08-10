import { router, useFocusEffect } from "expo-router";
import { useCallback, useState } from "react";
import { FlatList, Pressable, StyleSheet, Text, View } from "react-native";
import { newId, type ChatSummary } from "@wa/client-core";
import { useServices } from "../../src/ui/ServicesContext";

export default function Chats() {
  const { services } = useServices();
  const [items, setItems] = useState<ChatSummary[]>([]);

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

  function newConversation(): void {
    // Contacts-driven conversations arrive with T1.09; the shell starts a blank
    // local thread so the compose/outbox path is exercisable.
    router.push(`/thread/${newId()}`);
  }

  return (
    <View style={styles.container}>
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
      <Pressable style={styles.fab} onPress={newConversation} accessibilityLabel="New conversation">
        <Text style={styles.fabText}>＋</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
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
});
