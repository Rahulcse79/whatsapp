import * as ExpoSecureStore from "expo-secure-store";
import type { SecureStore } from "@wa/client-core";

// SecureStore port backed by Keychain (iOS) / Keystore (Android).
export const secureStore: SecureStore = {
  get(key: string): Promise<string | null> {
    return ExpoSecureStore.getItemAsync(key);
  },
  async set(key: string, value: string): Promise<void> {
    await ExpoSecureStore.setItemAsync(key, value);
  },
  async delete(key: string): Promise<void> {
    await ExpoSecureStore.deleteItemAsync(key);
  },
};
