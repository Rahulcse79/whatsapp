import { classifyMedia, parseMediaMessage, type MediaEnvelope } from "@wa/media-pipeline";
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
import { DownloadsPanel } from "../../../src/ui/media/DownloadsPanel";
import { Gallery } from "../../../src/ui/media/Gallery";
import { MediaMessage } from "../../../src/ui/media/MediaMessage";
import { useServices } from "../../../src/ui/ServicesContext";

export default function Thread() {
  const { services } = useServices();
  const params = useLocalSearchParams<{ id?: string }>();
  const conversationId = String(params.id ?? "");

  const [messages, setMessages] = useState<ThreadMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [gallery, setGallery] = useState<{ items: MediaEnvelope[]; startKey: string } | null>(null);

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

  // Every image/video in the thread, so the lightbox can page across them.
  const visuals: MediaEnvelope[] = [];
  for (const m of messages) {
    if (m.deleted) continue;
    const media = parseMediaMessage(m.body);
    if (media) for (const a of media.attachments) if (isVisual(a)) visuals.push(a);
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
          <MessageBubble message={item} onOpen={(env) => setGallery({ items: visuals, startKey: env.objectKey })} />
        )}
        ListEmptyComponent={<Text style={styles.empty}>Say hello 👋</Text>}
      />
      <DownloadsPanel />
      {gallery ? <Gallery items={gallery.items} startKey={gallery.startKey} onClose={() => setGallery(null)} /> : null}
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

function isVisual(env: MediaEnvelope): boolean {
  const kind = classifyMedia(env.mime);
  return kind === "image" || kind === "video";
}

/** MessageBubble renders a text message, or — when the decrypted body carries a
 *  media envelope — the attachment(s) plus any caption. */
function MessageBubble({ message, onOpen }: { message: ThreadMessage; onOpen: (env: MediaEnvelope) => void }) {
  const media = message.deleted ? null : parseMediaMessage(message.body);

  return (
    <View style={[styles.bubble, message.mine ? styles.mine : styles.theirs]}>
      {media ? (
        <View style={styles.mediaWrap}>
          {media.attachments.map((env) => (
            <MediaMessage key={env.objectKey} env={env} onOpen={onOpen} />
          ))}
          {media.caption ? <Text style={styles.body}>{media.caption}</Text> : null}
        </View>
      ) : (
        <Text style={styles.body}>{message.deleted ? "This message was deleted" : message.body}</Text>
      )}
      {message.mine ? <Text style={styles.meta}>{message.state}</Text> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  mediaWrap: { gap: 6 },
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
