import type { KeyValueStore } from "@wa/client-core";

// Web KeyValueStore: localStorage, for non-secret client-local preferences
// (drafts, mutes, favourites, saved replies, wallpaper). Secrets live in
// indexedDbSecureStore, never here.
//
// Every operation swallows its error and behaves as if the key were absent.
// localStorage throws in private-browsing modes and when the quota is full, and
// a preference read is never worth taking the app down for.
export const localKeyValueStore: KeyValueStore = {
  get(key: string): string | null {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  set(key: string, value: string): void {
    try {
      localStorage.setItem(key, value);
    } catch {
      /* quota exceeded or storage disabled — the preference just will not persist */
    }
  },
  remove(key: string): void {
    try {
      localStorage.removeItem(key);
    } catch {
      /* ignore */
    }
  },
};
