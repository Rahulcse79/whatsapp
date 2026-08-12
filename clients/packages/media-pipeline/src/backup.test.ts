import { describe, expect, it } from "vitest";
import { decryptBackup, encryptBackup, newBackupSalt, type BackupKeyDeriver } from "./backup";

// A deterministic stand-in for Argon2id: key = SHA-256(password || salt). NOT
// memory-hard — production injects argon2-browser; this exercises the archive
// crypto + the password→key→AES-GCM plumbing.
const fakeDeriver: BackupKeyDeriver = {
  async derive(password, salt) {
    const pw = new TextEncoder().encode(password);
    const material = new Uint8Array(pw.length + salt.length);
    material.set(pw, 0);
    material.set(salt, pw.length);
    return new Uint8Array(await crypto.subtle.digest("SHA-256", material));
  },
};

const archive = (n: number) => {
  const a = new Uint8Array(n);
  for (let i = 0; i < n; i++) a[i] = (i * 17 + 3) & 0xff;
  return a;
};

describe("encryptBackup / decryptBackup", () => {
  it("round-trips an archive under the same password + salt", async () => {
    const salt = newBackupSalt();
    const data = archive(5000);
    const ct = await encryptBackup(fakeDeriver, "correct horse", salt, data);
    expect(await decryptBackup(fakeDeriver, "correct horse", salt, ct)).toEqual(data);
  });

  it("does not leave the plaintext in the ciphertext", async () => {
    const salt = newBackupSalt();
    const marker = new Uint8Array(64).fill(0xcd);
    const ct = await encryptBackup(fakeDeriver, "pw", salt, marker);
    // No 64-byte run of 0xcd survives.
    expect(ct.includes(0xcd) && ctHasRun(ct, marker)).toBe(false);
  });

  it("a wrong password cannot restore (GCM tag fails)", async () => {
    const salt = newBackupSalt();
    const ct = await encryptBackup(fakeDeriver, "right", salt, archive(1000));
    await expect(decryptBackup(fakeDeriver, "wrong", salt, ct)).rejects.toBeTruthy();
  });

  it("a wrong salt cannot restore", async () => {
    const ct = await encryptBackup(fakeDeriver, "pw", newBackupSalt(), archive(1000));
    await expect(decryptBackup(fakeDeriver, "pw", newBackupSalt(), ct)).rejects.toBeTruthy();
  });

  it("rejects a deriver that returns the wrong key length", async () => {
    const bad: BackupKeyDeriver = { derive: () => Promise.resolve(new Uint8Array(16)) };
    await expect(encryptBackup(bad, "pw", newBackupSalt(), archive(10))).rejects.toThrow(/32 bytes/);
  });

  it("newBackupSalt is 16 random bytes", () => {
    const a = newBackupSalt();
    const b = newBackupSalt();
    expect(a.length).toBe(16);
    expect(a).not.toEqual(b);
  });
});

function ctHasRun(hay: Uint8Array, needle: Uint8Array): boolean {
  outer: for (let i = 0; i + needle.length <= hay.length; i++) {
    for (let j = 0; j < needle.length; j++) if (hay[i + j] !== needle[j]) continue outer;
    return true;
  }
  return false;
}
