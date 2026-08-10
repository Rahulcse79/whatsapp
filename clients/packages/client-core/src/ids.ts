// Time-ordered client identifiers used as message/frame idempotency keys. Not a
// spec UUIDv7 (the server mints those); this is a lexicographically sortable,
// collision-resistant id good enough to key the outbox and dedupe resends. now
// + rng are injected for deterministic tests.

export function newId(now: () => number = Date.now, rng: () => number = Math.random): string {
  const ts = Math.floor(now()).toString(16).padStart(12, "0");
  let rand = "";
  for (let i = 0; i < 4; i++) {
    rand += Math.floor(rng() * 0x10000)
      .toString(16)
      .padStart(4, "0");
  }
  return `${ts}-${rand}`;
}

/** E.164-ish sanity check (not full validation): + and 8–15 digits. */
export function isValidPhone(phone: string): boolean {
  return /^\+[1-9]\d{7,14}$/.test(phone.trim());
}
