// Gallery — the full-screen image/video lightbox on mobile, in an RN Modal.
// Given the thread's visual attachments and the tapped one, it shows it large
// with prev/next paging. Image playback is native (<Image> + data URI); video
// playback needs a native player (deferred) so a video slide shows a poster and
// a Save affordance instead.

import { classifyMedia, downloadName, formatBytes, type MediaEnvelope } from "@wa/media-pipeline";
import { useState } from "react";
import { ActivityIndicator, Image, Modal, Pressable, StyleSheet, Text, View } from "react-native";
import { useDownload, useMediaService } from "./MediaContext";

export function Gallery({ items, startKey, onClose }: { items: MediaEnvelope[]; startKey: string; onClose: () => void }) {
  const [index, setIndex] = useState(() => Math.max(0, items.findIndex((e) => e.objectKey === startKey)));
  const go = (delta: number): void => setIndex((i) => (i + delta + items.length) % items.length);
  const current = items[index];

  return (
    <Modal visible transparent animationType="fade" onRequestClose={onClose}>
      <View style={styles.backdrop}>
        <Pressable style={styles.close} onPress={onClose} accessibilityLabel="Close">
          <Text style={styles.closeText}>✕</Text>
        </Pressable>
        {current ? <Slide env={current} /> : null}
        {items.length > 1 ? (
          <>
            <Pressable style={[styles.nav, styles.left]} onPress={() => go(-1)} accessibilityLabel="Previous">
              <Text style={styles.navText}>‹</Text>
            </Pressable>
            <Pressable style={[styles.nav, styles.right]} onPress={() => go(1)} accessibilityLabel="Next">
              <Text style={styles.navText}>›</Text>
            </Pressable>
            <Text style={styles.count}>
              {index + 1} / {items.length}
            </Text>
          </>
        ) : null}
      </View>
    </Modal>
  );
}

function Slide({ env }: { env: MediaEnvelope }) {
  const svc = useMediaService();
  const { item, uri, retry } = useDownload(env);

  if (item.state === "error") {
    return (
      <Pressable style={styles.center} onPress={retry}>
        <Text style={styles.hint}>⟳ Retry download</Text>
      </Pressable>
    );
  }
  if (!uri) return <ActivityIndicator size="large" color="#fff" />;

  if (classifyMedia(env.mime) === "video") {
    return (
      <View style={styles.center}>
        <Image source={{ uri }} style={styles.media} resizeMode="contain" />
        <Text style={styles.hint}>Video · {formatBytes(env.sizeBytes)}</Text>
        {item.bytes ? (
          <Pressable style={styles.save} onPress={() => item.bytes && void svc.handlers.onSave?.(env, item.bytes)}>
            <Text style={styles.saveText}>Save</Text>
          </Pressable>
        ) : null}
      </View>
    );
  }
  return <Image source={{ uri }} style={styles.media} resizeMode="contain" accessibilityLabel={downloadName(env)} />;
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: "rgba(0,0,0,0.94)", alignItems: "center", justifyContent: "center" },
  center: { alignItems: "center", justifyContent: "center", gap: 12 },
  media: { width: "92%", height: "80%" },
  hint: { color: "#fff", fontSize: 14 },
  close: { position: "absolute", top: 44, right: 20, zIndex: 2 },
  closeText: { color: "#fff", fontSize: 26 },
  nav: { position: "absolute", top: "50%", width: 52, height: 72, alignItems: "center", justifyContent: "center", backgroundColor: "rgba(255,255,255,0.12)" },
  left: { left: 8 },
  right: { right: 8 },
  navText: { color: "#fff", fontSize: 40 },
  count: { position: "absolute", bottom: 30, color: "#fff", fontSize: 13 },
  save: { backgroundColor: "#128C7E", paddingHorizontal: 16, paddingVertical: 8, borderRadius: 8 },
  saveText: { color: "#fff", fontSize: 14 },
});
