// Time-ordered client identifiers used as message/frame idempotency keys. These
// are spec UUIDv7 — the server REQUIRES v7 for message/frame ids
// (internal/platform/id.Parse rejects other versions), and the 48-bit ms
// timestamp prefix keeps ids lexicographically time-ordered for the outbox.
// now + rng are injected for deterministic tests.

export function newId(now: () => number = Date.now, rng: () => number = Math.random): string {
  const ts = Math.floor(now()) % 0x1000000000000; // 48-bit ms timestamp
  const tsHex = ts.toString(16).padStart(12, "0");
  const rhex = (n: number): string => {
    let s = "";
    for (let i = 0; i < n; i++) s += Math.floor(rng() * 16).toString(16);
    return s;
  };
  const variant = "89ab".charAt(Math.floor(rng() * 4)); // RFC-4122 variant
  return `${tsHex.slice(0, 8)}-${tsHex.slice(8, 12)}-7${rhex(3)}-${variant}${rhex(3)}-${rhex(12)}`;
}

/** E.164-ish sanity check (not full validation): + and 8–15 digits. */
export function isValidPhone(phone: string): boolean {
  return /^\+[1-9]\d{7,14}$/.test(phone.trim());
}
