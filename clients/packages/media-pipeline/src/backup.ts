// Encrypted-backup archive crypto (FR-SYNC-04, relay-model ADR-001). The whole
// backup archive is sealed on the device under a key derived from the user's
// backup password; the server stores only the ciphertext + size and never sees
// the key. Restore re-derives the key from the password + the stored salt.
//
// The archive cipher is the same chunked AES-256-GCM as media (streams a multi-GB
// archive without holding it decrypted in memory). The one new primitive is the
// password KDF: Argon2id — memory-hard, so a stolen ciphertext resists offline
// guessing — which WebCrypto does NOT provide, so it is an injected port (web:
// argon2-browser WASM; React Native: a native module). Never a plain PBKDF2/
// SHA fallback in production.

import { decryptMedia, encryptWithKey } from "./mediaCrypto";

/** BackupKeyDeriver stretches the backup password + salt into the 32-byte archive
 *  key via Argon2id. Injected because Argon2id isn't in WebCrypto. */
export interface BackupKeyDeriver {
  derive(password: string, salt: Uint8Array): Promise<Uint8Array>;
}

/** newBackupSalt returns a fresh 16-byte salt. A salt is not secret — it is
 *  stored (client-side) alongside the backup so restore re-derives the same key. */
export function newBackupSalt(): Uint8Array {
  return crypto.getRandomValues(new Uint8Array(16));
}

/** encryptBackup seals `archive` under the password-derived key and returns the
 *  ciphertext to upload. Persist the `salt` with it (needed to restore). */
export async function encryptBackup(
  deriver: BackupKeyDeriver,
  password: string,
  salt: Uint8Array,
  archive: Uint8Array,
): Promise<Uint8Array> {
  const key = await deriver.derive(password, salt);
  if (key.length !== 32) throw new Error(`backup key must be 32 bytes, got ${key.length}`);
  return encryptWithKey(key, archive);
}

/** decryptBackup restores the archive. A wrong password derives a wrong key, so
 *  AES-GCM's tag fails and the promise rejects — no false "restore". */
export async function decryptBackup(
  deriver: BackupKeyDeriver,
  password: string,
  salt: Uint8Array,
  ciphertext: Uint8Array,
): Promise<Uint8Array> {
  const key = await deriver.derive(password, salt);
  return decryptMedia(key, ciphertext);
}
