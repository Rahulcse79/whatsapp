// DownloadsPanel — the mobile "download manager" strip. Lists in-flight, queued,
// and failed transfers with a retry affordance; hides when nothing is active.

import { ActivityIndicator, Pressable, StyleSheet, Text, View } from "react-native";
import { useDownloadQueue, useMediaService } from "./MediaContext";

export function DownloadsPanel() {
  const svc = useMediaService();
  const items = useDownloadQueue().filter((i) => i.state !== "ready");
  if (items.length === 0) return null;

  return (
    <View style={styles.panel}>
      <Text style={styles.head}>Transfers</Text>
      {items.map((i) => (
        <View key={i.objectKey} style={styles.row}>
          <Text style={styles.name} numberOfLines={1}>
            {i.objectKey.split("/").pop() ?? i.objectKey}
          </Text>
          {i.state === "error" ? (
            <Pressable onPress={() => svc.retry(i.objectKey)}>
              <Text style={styles.retry}>⟳ Retry</Text>
            </Pressable>
          ) : (
            <ActivityIndicator size="small" />
          )}
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  panel: { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: "#e2e2e2", backgroundColor: "#fafafa", paddingHorizontal: 12, paddingVertical: 6, maxHeight: 130 },
  head: { fontSize: 12, fontWeight: "600", marginBottom: 4 },
  row: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8, paddingVertical: 2 },
  name: { flex: 1, fontSize: 12, color: "#333" },
  retry: { fontSize: 12, color: "#128C7E" },
});
