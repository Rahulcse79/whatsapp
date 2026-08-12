import { router } from "expo-router";
import { useEffect, useState, type ReactNode } from "react";
import { FlatList, Pressable, StyleSheet, Text, TextInput, View } from "react-native";
import { SNIPPET_CLOSE, SNIPPET_OPEN, type SearchHit } from "@wa/client-core";
import { useServices } from "../../src/ui/ServicesContext";

// Full-text search over the local decrypted store (ADR-005: client-side FTS5;
// the server holds only ciphertext). Debounced as-you-type; a result opens its
// conversation.
export default function SearchScreen() {
  const { services } = useServices();
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);

  useEffect(() => {
    const q = query.trim();
    if (!q) {
      setHits([]);
      return;
    }
    let alive = true;
    const handle = setTimeout(() => {
      services
        .search(q)
        .then((r) => {
          if (alive) setHits(r);
        })
        .catch(() => {
          /* transient read error; the next keystroke retries */
        });
    }, 150);
    return () => {
      alive = false;
      clearTimeout(handle);
    };
  }, [query, services]);

  return (
    <View style={styles.container}>
      <TextInput
        style={styles.input}
        value={query}
        onChangeText={setQuery}
        placeholder="Search messages"
        autoFocus
        autoCorrect={false}
        autoCapitalize="none"
        returnKeyType="search"
      />
      <FlatList
        data={hits}
        keyExtractor={(h) => h.msgUuid}
        keyboardShouldPersistTaps="handled"
        renderItem={({ item }) => (
          <Pressable style={styles.row} onPress={() => router.push(`/thread/${item.conversationId}`)}>
            <Text style={styles.title} numberOfLines={1}>
              {item.conversationTitle}
            </Text>
            <Text style={styles.snippet} numberOfLines={2}>
              {highlight(item.snippet)}
            </Text>
          </Pressable>
        )}
        ListEmptyComponent={query.trim() ? <Text style={styles.empty}>No matches.</Text> : null}
        contentContainerStyle={hits.length === 0 ? styles.emptyContainer : undefined}
      />
    </View>
  );
}

/** highlight wraps SNIPPET_OPEN/CLOSE-delimited matched terms in a bold <Text>. */
function highlight(snippet: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  let rest = snippet;
  let key = 0;
  for (;;) {
    const open = rest.indexOf(SNIPPET_OPEN);
    const close = open === -1 ? -1 : rest.indexOf(SNIPPET_CLOSE, open + SNIPPET_OPEN.length);
    if (open === -1 || close === -1) {
      nodes.push(rest);
      break;
    }
    if (open > 0) nodes.push(rest.slice(0, open));
    nodes.push(
      <Text key={key++} style={styles.mark}>
        {rest.slice(open + SNIPPET_OPEN.length, close)}
      </Text>,
    );
    rest = rest.slice(close + SNIPPET_CLOSE.length);
  }
  return nodes;
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  input: {
    margin: 12,
    paddingHorizontal: 14,
    paddingVertical: 10,
    borderRadius: 10,
    backgroundColor: "#f0f0f0",
    fontSize: 16,
  },
  row: { paddingHorizontal: 20, paddingVertical: 14, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: "#e2e2e2" },
  title: { fontSize: 16, fontWeight: "600" },
  snippet: { fontSize: 14, color: "#555", marginTop: 2 },
  mark: { fontWeight: "700", color: "#111" },
  empty: { textAlign: "center", color: "#999", fontSize: 15 },
  emptyContainer: { flexGrow: 1, alignItems: "center", justifyContent: "center", padding: 24 },
});
