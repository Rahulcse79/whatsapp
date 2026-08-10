import type { Cancel, Scheduler } from "../core/ports";

// Real timers backing the Scheduler port. The core injects this in production
// and a manual clock in tests.
export const rnScheduler: Scheduler = {
  setTimeout(fn: () => void, ms: number): Cancel {
    const id = setTimeout(fn, ms);
    return () => clearTimeout(id);
  },
  now(): number {
    return Date.now();
  },
};
