// App-wide light/dark theming (T5.15). The choice is "system" | "light" |
// "dark", persisted in localStorage. We resolve "system" against the OS via
// matchMedia and stamp the result on <html data-theme="…"> so styles.css can key
// its palette off it; in system mode we also react to OS changes live.

export type ThemeChoice = "system" | "light" | "dark";

const KEY = "wa.theme";
let mediaListener: ((e: MediaQueryListEvent) => void) | null = null;

export function getTheme(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "light" || v === "dark" || v === "system") return v;
  } catch {
    /* default below */
  }
  return "system";
}

function prefersDark(): boolean {
  return typeof matchMedia === "function" && matchMedia("(prefers-color-scheme: dark)").matches;
}

function resolve(choice: ThemeChoice): "light" | "dark" {
  return choice === "system" ? (prefersDark() ? "dark" : "light") : choice;
}

/** applyTheme stamps the resolved theme on <html> and, in system mode, keeps it
 *  in sync with OS changes. Call once at startup and on every setTheme. */
export function applyTheme(choice: ThemeChoice = getTheme()): void {
  document.documentElement.setAttribute("data-theme", resolve(choice));

  // Only listen for OS changes while following the system.
  if (typeof matchMedia !== "function") return;
  const mq = matchMedia("(prefers-color-scheme: dark)");
  if (mediaListener) mq.removeEventListener?.("change", mediaListener);
  mediaListener = null;
  if (choice === "system") {
    mediaListener = () => document.documentElement.setAttribute("data-theme", resolve("system"));
    mq.addEventListener?.("change", mediaListener);
  }
}

export function setTheme(choice: ThemeChoice): void {
  try {
    localStorage.setItem(KEY, choice);
  } catch {
    /* non-persistent this session — still applied below */
  }
  applyTheme(choice);
}
