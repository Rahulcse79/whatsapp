// Interactive messages (T13.02): a structured message body carrying tap-able
// buttons (quick-replies / callbacks) and an optional rich card. For user↔user
// interactive messages the whole thing rides the sealed E2EE body (like polls);
// bots send them server-side. Pure + framework-free — the platform renders the
// model and reports taps.

export interface InteractiveButton {
  id: string;
  label: string;
  /** payload sent back on tap (to a bot); falls back to the label as a quick-reply */
  payload?: string;
  /** "reply" sends the payload/label as a normal message; "callback" pings a bot */
  kind?: "reply" | "callback" | "url";
  /** for kind "url" — opened on tap */
  url?: string;
}

export interface InteractiveCard {
  title: string;
  subtitle?: string;
  imageUrl?: string;
}

export interface InteractiveMessage {
  t: "interactive";
  text: string;
  buttons: InteractiveButton[];
  card?: InteractiveCard;
}

export const MAX_BUTTONS = 3;
export const MAX_LABEL = 40;
export const MAX_TEXT = 1000;

/** validateInteractive returns an error string, or null when valid. */
export function validateInteractive(text: string, buttons: InteractiveButton[]): string | null {
  if (text.trim() === "" || text.length > MAX_TEXT) return "Message text is required (max 1000 chars).";
  if (buttons.length === 0 || buttons.length > MAX_BUTTONS) return `Add between 1 and ${MAX_BUTTONS} buttons.`;
  for (const b of buttons) {
    if (b.label.trim() === "" || b.label.length > MAX_LABEL) return `Each button needs a label (max ${MAX_LABEL} chars).`;
    if (b.kind === "url" && !/^https?:\/\//i.test(b.url ?? "")) return "A link button needs a valid https URL.";
  }
  return null;
}

/** encodeInteractive builds the sealed body for an interactive message (throws on
 *  invalid input). */
export function encodeInteractive(text: string, buttons: InteractiveButton[], card?: InteractiveCard): string {
  const err = validateInteractive(text, buttons);
  if (err) throw new Error(err);
  const msg: InteractiveMessage = { t: "interactive", text: text.trim(), buttons, ...(card ? { card } : {}) };
  return JSON.stringify(msg);
}

/** parseInteractive reads a decrypted body, or null if it isn't an interactive
 *  message. Defensive: it drops malformed buttons rather than throwing. */
export function parseInteractive(body: string): InteractiveMessage | null {
  try {
    const o = JSON.parse(body) as Partial<InteractiveMessage>;
    if (!o || o.t !== "interactive" || typeof o.text !== "string" || !Array.isArray(o.buttons)) return null;
    const buttons: InteractiveButton[] = [];
    for (const b of o.buttons) {
      if (b && typeof b.id === "string" && typeof b.label === "string" && b.label.trim() !== "") {
        buttons.push({
          id: b.id,
          label: b.label,
          payload: typeof b.payload === "string" ? b.payload : undefined,
          kind: b.kind === "callback" || b.kind === "url" ? b.kind : "reply",
          url: typeof b.url === "string" ? b.url : undefined,
        });
      }
    }
    if (buttons.length === 0) return null;
    const card = o.card && typeof o.card.title === "string" ? o.card : undefined;
    return { t: "interactive", text: o.text, buttons, card };
  } catch {
    return null;
  }
}

/** replyTextFor is what a tapped "reply" button sends as a message. */
export function replyTextFor(button: InteractiveButton): string {
  return (button.payload ?? button.label).trim();
}
