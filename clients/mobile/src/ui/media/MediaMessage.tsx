// MediaMessage renders one attachment in a chat bubble on mobile, dispatching on
// media kind: image/video → tappable thumbnail (blurhash colour → decrypted
// image, opens the gallery), audio → voice-note row, document → a file tile.
// Transfer state comes from the shared DownloadManager via useDownload; image
// bytes are delivered to <Image> as a data URI.

import {
  blurhashCssColor,
  classifyMedia,
  downloadName,
  formatBytes,
  formatDuration,
  isVoiceNote,
  type MediaEnvelope,
} from "@wa/media-pipeline";
import { ActivityIndicator, Image, Pressable, StyleSheet, Text, View } from "react-native";
import { useDownload, useMediaService } from "./MediaContext";

export function MediaMessage({ env, onOpen }: { env: MediaEnvelope; onOpen?: (env: MediaEnvelope) => void }) {
  switch (classifyMedia(env.mime)) {
    case "image":
    case "video":
      return <VisualBubble env={env} onOpen={onOpen} video={classifyMedia(env.mime) === "video"} />;
    case "audio":
      return <VoiceNote env={env} />;
    default:
      return <DocumentTile env={env} />;
  }
}

function VisualBubble({ env, onOpen, video }: { env: MediaEnvelope; onOpen?: (env: MediaEnvelope) => void; video: boolean }) {
  const { item, uri, retry } = useDownload(env);
  const ratio = env.width && env.height ? env.width / env.height : 4 / 3;

  return (
    <Pressable
      style={[styles.visual, { aspectRatio: ratio, backgroundColor: blurhashCssColor(env.blurhash) }]}
      onPress={() => (item.state === "error" ? retry() : uri ? onOpen?.(env) : undefined)}
      accessibilityLabel={video ? "Play video" : "Open image"}
    >
      {uri ? <Image source={{ uri }} style={styles.full} resizeMode="cover" /> : null}
      {video && uri ? <Text style={styles.play}>▶</Text> : null}
      {item.state === "downloading" || item.state === "queued" ? (
        <View style={styles.overlay}>
          <ActivityIndicator color="#fff" />
        </View>
      ) : null}
      {item.state === "error" ? (
        <View style={styles.overlay}>
          <Text style={styles.retry}>⟳ Retry</Text>
        </View>
      ) : null}
    </Pressable>
  );
}

function VoiceNote({ env }: { env: MediaEnvelope }) {
  const svc = useMediaService();
  const { item, retry } = useDownload(env);
  const voice = isVoiceNote(env);
  const ready = item.state === "ready" && item.bytes;

  return (
    <View style={styles.voice}>
      <Pressable
        style={styles.voiceBtn}
        disabled={!ready && item.state !== "error"}
        onPress={() => {
          if (item.state === "error") retry();
          else if (ready && item.bytes) void svc.handlers.onPlay?.(env, item.bytes);
        }}
        accessibilityLabel={voice ? "Play voice message" : "Play audio"}
      >
        {item.state === "downloading" || item.state === "queued" ? (
          <ActivityIndicator />
        ) : (
          <Text style={styles.voiceIcon}>{item.state === "error" ? "⟳" : "▶"}</Text>
        )}
      </Pressable>
      <View style={styles.voiceMeta}>
        <Text style={styles.voiceLabel}>{voice ? "Voice message" : "Audio"}</Text>
        {env.durationMs ? <Text style={styles.sub}>{formatDuration(env.durationMs)}</Text> : null}
      </View>
    </View>
  );
}

function DocumentTile({ env }: { env: MediaEnvelope }) {
  const svc = useMediaService();
  const { item, retry } = useDownload(env);
  const name = downloadName(env);
  const ready = item.state === "ready" && item.bytes;

  return (
    <View style={styles.doc}>
      <Text style={styles.docIcon}>📄</Text>
      <View style={styles.docMeta}>
        <Text style={styles.docName} numberOfLines={1}>
          {name}
        </Text>
        <Text style={styles.sub}>{formatBytes(env.sizeBytes)}</Text>
      </View>
      {item.state === "downloading" || item.state === "queued" ? (
        <ActivityIndicator />
      ) : (
        <Pressable
          style={styles.docBtn}
          onPress={() => {
            if (item.state === "error") retry();
            else if (ready && item.bytes) void svc.handlers.onSave?.(env, item.bytes);
          }}
        >
          <Text style={styles.docBtnText}>{item.state === "error" ? "⟳" : "Save"}</Text>
        </Pressable>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  visual: { width: 240, maxWidth: "80%", borderRadius: 10, overflow: "hidden", alignItems: "center", justifyContent: "center" },
  full: { ...StyleSheet.absoluteFillObject, width: "100%", height: "100%" },
  play: { fontSize: 34, color: "#fff", textShadowColor: "rgba(0,0,0,0.6)", textShadowRadius: 6 },
  overlay: { ...StyleSheet.absoluteFillObject, alignItems: "center", justifyContent: "center", backgroundColor: "rgba(0,0,0,0.15)" },
  retry: { color: "#fff", backgroundColor: "rgba(0,0,0,0.6)", paddingHorizontal: 10, paddingVertical: 4, borderRadius: 12, overflow: "hidden" },
  voice: { flexDirection: "row", alignItems: "center", gap: 10, minWidth: 200 },
  voiceBtn: { width: 40, height: 40, borderRadius: 20, backgroundColor: "#128C7E", alignItems: "center", justifyContent: "center" },
  voiceIcon: { color: "#fff", fontSize: 18 },
  voiceMeta: { flex: 1 },
  voiceLabel: { fontSize: 14 },
  doc: { flexDirection: "row", alignItems: "center", gap: 10, minWidth: 220 },
  docIcon: { fontSize: 26 },
  docMeta: { flex: 1, minWidth: 0 },
  docName: { fontSize: 14 },
  docBtn: { backgroundColor: "#128C7E", paddingHorizontal: 12, paddingVertical: 6, borderRadius: 8 },
  docBtnText: { color: "#fff", fontSize: 13 },
  sub: { fontSize: 12, color: "#777", marginTop: 2 },
});
