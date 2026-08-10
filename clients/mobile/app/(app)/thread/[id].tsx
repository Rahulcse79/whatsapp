import { useFocusEffect, useLocalSearchParams } from "expo-router";
import { useCallback, useState } from "react";
import {
  FlatList,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import type { ThreadMessage } from "@wa/client-core";
import { useServices } from "../../../src/ui/ServicesContext";

export default function Thread() {
  const { services } = useServices();
  const params = useLocalSearchParams<{ id?: string }>();
  const conversationId = String(params.id ?? "");

  const [messages, setMessages] = useState<ThreadMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);

  const load = useCallback(() => {
    services.messages
      .thread(conversationId)
      .then(setMessages)
      .catch(() => {
        /* transient read error */
      });
  }, [services, conversationId]);

  useFocusEffect(
    useCallback(() => {
      load();
    }, [load]),
  );

  async function onSend(): Promise<void> {
    const text = draft.trim();
    if (!text || sending) return;
    setSending(true);
    setDraft("");
    try {
      await services.sendText(conversationId, text);
      load();
    } finally {
      setSending(false);
    }
  }

  return (
    <KeyboardAvoidingView
      style={styles.container}
      behavior={Platform.OS === "ios" ? "padding" : undefined}
      keyboardVerticalOffset={90}
    >
      <FlatList
        data={messages}
        keyExtractor={(m) => m.msgUuid}
        contentContainerStyle={styles.list}
        renderItem={({ item }) => (
          <View style={[styles.bubble, item.mine ? styles.mine : styles.theirs]}>
            <Text style={styles.body}>{item.deleted ? "This message was deleted" : item.body}</Text>
            {item.mine ? <Text style={styles.meta}>{item.state}</Text> : null}
          </View>
        )}
        ListEmptyComponent={<Text style={styles.empty}>Say hello 👋</Text>}
      />
      <View style={styles.composer}>
        <TextInput
          style={styles.input}
          value={draft}
          onChangeText={setDraft}
          placeholder="Message"
          multiline
          editable={!sending}
        />
        <Pressable style={styles.send} onPress={onSend} disabled={sending} accessibilityLabel="Send">
          <Text style={styles.sendText}>➤</Text>
        </Pressable>
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  list: { padding: 12, gap: 6 },
  bubble: { maxWidth: "80%", borderRadius: 12, paddingHorizontal: 12, paddingVertical: 8 },
  mine: { alignSelf: "flex-end", backgroundColor: "#DCF8C6" },
  theirs: { alignSelf: "flex-start", backgroundColor: "#F0F0F0" },
  body: { fontSize: 16 },
  meta: { fontSize: 11, color: "#5a7", marginTop: 2, textAlign: "right" },
  empty: { textAlign: "center", color: "#999", marginTop: 40 },
  composer: { flexDirection: "row", alignItems: "flex-end", padding: 8, gap: 8, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: "#e2e2e2" },
  input: { flex: 1, maxHeight: 120, borderWidth: 1, borderColor: "#ccc", borderRadius: 20, paddingHorizontal: 14, paddingVertical: 8, fontSize: 16 },
  send: { width: 44, height: 44, borderRadius: 22, backgroundColor: "#128C7E", alignItems: "center", justifyContent: "center" },
  sendText: { color: "#fff", fontSize: 18 },
});
