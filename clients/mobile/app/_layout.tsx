import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { MediaProvider } from "../src/ui/media/MediaContext";
import { ServicesProvider } from "../src/ui/ServicesContext";

// Root layout: boot services once, then a native stack. Route groups (auth)/
// (app) are transparent in URLs — screens live at /login, /verify, /chats,
// /thread/[id].
export default function RootLayout() {
  return (
    <ServicesProvider>
      <MediaProvider>
        <StatusBar style="auto" />
        <Stack screenOptions={{ headerTitleAlign: "center" }}>
          <Stack.Screen name="index" options={{ headerShown: false }} />
          <Stack.Screen name="(auth)/login" options={{ title: "Sign in" }} />
          <Stack.Screen name="(auth)/verify" options={{ title: "Verify" }} />
          <Stack.Screen name="(app)/chats" options={{ title: "Chats" }} />
          <Stack.Screen name="(app)/search" options={{ title: "Search" }} />
          <Stack.Screen name="(app)/thread/[id]" options={{ title: "Conversation" }} />
        </Stack>
      </MediaProvider>
    </ServicesProvider>
  );
}
