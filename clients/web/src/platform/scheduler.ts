import type { Cancel, Scheduler } from "@wa/client-core";

// Real timers backing the Scheduler port (WsClient injects a manual clock in tests).
export const webScheduler: Scheduler = {
  setTimeout(fn: () => void, ms: number): Cancel {
    const id = setTimeout(fn, ms);
    return () => clearTimeout(id);
  },
  now(): number {
    return Date.now();
  },
};
