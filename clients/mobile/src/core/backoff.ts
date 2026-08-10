// Reconnect backoff. Full jitter spreads a herd of clients reconnecting after a
// deploy/drain evenly across the window instead of synchronising retries
// (websocket-protocol.md §6, close 1012).

export interface BackoffConfig {
  baseMs: number;
  capMs: number;
  factor: number;
}

export const defaultBackoff: BackoffConfig = { baseMs: 500, capMs: 30_000, factor: 2 };

/**
 * backoffDelay returns a "full jitter" delay for a 0-based attempt: a uniform
 * random point in [0, min(cap, base·factor^attempt)]. The exponential ceiling
 * grows per attempt and is clamped by cap; the jitter is `rng()` (injected for
 * deterministic tests).
 */
export function backoffDelay(
  attempt: number,
  cfg: BackoffConfig = defaultBackoff,
  rng: () => number = Math.random,
): number {
  const ceiling = Math.min(cfg.capMs, cfg.baseMs * Math.pow(cfg.factor, Math.max(0, attempt)));
  return Math.round(rng() * ceiling);
}
