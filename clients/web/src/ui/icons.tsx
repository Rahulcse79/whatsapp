import type { ReactNode } from "react";

/** WhatsApp-style monochrome line icons. One <Icon name size> renders a 24px
 *  SVG in currentColor, so buttons colour them via CSS. Replaces the emoji glyphs
 *  that made the UI read as a clone rather than the real app. */
export type IconName =
  | "back"
  | "search"
  | "menu"
  | "attach"
  | "emoji"
  | "send"
  | "mic"
  | "camera"
  | "phone"
  | "video"
  | "newchat"
  | "community"
  | "updates"
  | "channel"
  | "group"
  | "contacts"
  | "settings"
  | "mute"
  | "bell"
  | "wallpaper"
  | "download"
  | "star"
  | "forward"
  | "reply"
  | "copy"
  | "trash"
  | "close"
  | "plus"
  | "check"
  | "checkDouble"
  | "clock"
  | "chats"
  | "info"
  | "archive";

const PATHS: Record<IconName, ReactNode> = {
  back: <path d="M15 19l-7-7 7-7" />,
  search: (
    <>
      <circle cx="11" cy="11" r="7" />
      <path d="M21 21l-4.35-4.35" />
    </>
  ),
  menu: (
    <g fill="currentColor" stroke="none">
      <circle cx="12" cy="5" r="1.7" />
      <circle cx="12" cy="12" r="1.7" />
      <circle cx="12" cy="19" r="1.7" />
    </g>
  ),
  attach: <path d="M21.44 11.05l-9.19 9.19a5 5 0 0 1-7.07-7.07l9.19-9.19a3.5 3.5 0 0 1 4.95 4.95l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48" />,
  emoji: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M8.5 14.5s1.4 2 3.5 2 3.5-2 3.5-2" />
      <circle cx="9" cy="10" r="1" fill="currentColor" stroke="none" />
      <circle cx="15" cy="10" r="1" fill="currentColor" stroke="none" />
    </>
  ),
  send: <path d="M3.4 20.4l17.45-8a.5.5 0 0 0 0-.9L3.4 3.6a.5.5 0 0 0-.7.5l1.3 6.9 9 1-9 1-1.3 6.9a.5.5 0 0 0 .7.5z" fill="currentColor" stroke="none" />,
  mic: (
    <>
      <rect x="9" y="3" width="6" height="11" rx="3" />
      <path d="M5 11a7 7 0 0 0 14 0" />
      <path d="M12 18v3" />
      <path d="M8 21h8" />
    </>
  ),
  camera: (
    <>
      <path d="M4 8h3l1.6-2h6.8L17 8h3v11H4z" />
      <circle cx="12" cy="12.5" r="3.4" />
    </>
  ),
  phone: (
    <path d="M22 16.9v3a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3 19.5 19.5 0 0 1-6-6 19.8 19.8 0 0 1-3-8.6A2 2 0 0 1 4.1 2h3a2 2 0 0 1 2 1.7c.1 1 .4 2 .7 2.9a2 2 0 0 1-.5 2.1L8.1 9.9a16 16 0 0 0 6 6l1.2-1.2a2 2 0 0 1 2.1-.5c.9.3 1.9.6 2.9.7a2 2 0 0 1 1.7 2z" />
  ),
  video: (
    <>
      <rect x="2" y="6" width="13" height="12" rx="2.5" />
      <path d="M22 8.5l-5 3.5 5 3.5z" />
    </>
  ),
  newchat: (
    <>
      <path d="M21 11.5a8.4 8.4 0 0 1-8.5 8.5 8.6 8.6 0 0 1-3.8-.9L3 21l1.9-5.7A8.4 8.4 0 0 1 12.5 3 8.4 8.4 0 0 1 21 11.5z" />
      <path d="M12.5 8.5v6M9.5 11.5h6" />
    </>
  ),
  community: (
    // A group of people. The previous glyph was a rounded rect with stray
    // strokes and a dot, which read as nothing in particular at rail size.
    <>
      <circle cx="9" cy="8.5" r="3.1" />
      <path d="M3.4 19.4c0-3.1 2.5-4.9 5.6-4.9s5.6 1.8 5.6 4.9" />
      <circle cx="17.2" cy="10.2" r="2.2" />
      <path d="M15.6 14.9c2.7-.4 5 1.1 5 4.5" />
    </>
  ),
  updates: (
    // WhatsApp's Status mark is a ring of a few long arcs, not a dotted line:
    // longer dashes with round caps read as segments at 20-24px.
    <circle cx="12" cy="12" r="9" strokeDasharray="8.6 3.6" strokeWidth={2.1} />
  ),
  channel: (
    <>
      <path d="M4 10v4a1 1 0 0 0 1 1h3l5 4V5L9 9H5a1 1 0 0 0-1 1z" />
      <path d="M17 8.5a4 4 0 0 1 0 7" />
    </>
  ),
  group: (
    <>
      <circle cx="9" cy="8" r="3.4" />
      <path d="M3 20a6 6 0 0 1 12 0" />
      <circle cx="17" cy="9" r="2.4" />
      <path d="M16 14.6A4.6 4.6 0 0 1 21.5 19" />
    </>
  ),
  contacts: (
    <>
      <circle cx="12" cy="8" r="4" />
      <path d="M4 21a8 8 0 0 1 16 0" />
    </>
  ),
  settings: (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 13.5a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-2.7 1.1V21a2 2 0 0 1-4 0v-.1a1.6 1.6 0 0 0-2.7-1.1l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.6 1.6 0 0 0-1.1-2.7H2a2 2 0 0 1 0-4h.1A1.6 1.6 0 0 0 3.2 6.4l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.6 1.6 0 0 0 2.7-1.1V2a2 2 0 0 1 4 0v.1a1.6 1.6 0 0 0 2.7 1.1l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.4 1.7 1.6 1.6 0 0 0 1.5 1.1H22a2 2 0 0 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1z" />
    </>
  ),
  mute: (
    <>
      <path d="M13.7 21a2 2 0 0 1-3.4 0" />
      <path d="M18.6 13A17 17 0 0 1 18 8" />
      <path d="M6.3 6.3A6 6 0 0 0 6 8c0 7-3 9-3 9h14" />
      <path d="M18 8a6 6 0 0 0-9.3-5" />
      <path d="M2 2l20 20" />
    </>
  ),
  bell: (
    <>
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.7 21a2 2 0 0 1-3.4 0" />
    </>
  ),
  wallpaper: (
    <>
      <rect x="3" y="3" width="18" height="18" rx="2.5" />
      <circle cx="8.5" cy="8.5" r="1.6" />
      <path d="M21 15l-5-5L5 21" />
    </>
  ),
  download: (
    <>
      <path d="M12 3v12" />
      <path d="M7 11l5 5 5-5" />
      <path d="M5 21h14" />
    </>
  ),
  star: <path d="M12 2.5l2.9 6 6.6.6-5 4.4 1.5 6.5L12 17l-6 3 1.5-6.5-5-4.4 6.6-.6z" fill="currentColor" stroke="none" />,
  forward: <path d="M13 8V4l8 8-8 8v-4C7 16 4 18 3 21c0-7 3-12 10-13z" />,
  reply: <path d="M11 8V4L3 12l8 8v-4c6-1 9 1 10 4 0-7-3-12-10-12z" />,
  copy: (
    <>
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V5a2 2 0 0 1 2-2h8" />
    </>
  ),
  trash: (
    <>
      <path d="M4 7h16" />
      <path d="M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2" />
      <path d="M6 7l1 13a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-13" />
    </>
  ),
  close: <path d="M18 6L6 18M6 6l12 12" />,
  plus: <path d="M12 5v14M5 12h14" />,
  check: <path d="M20 6L9 17l-5-5" />,
  checkDouble: (
    <>
      <path d="M1.5 12.5l4 4 8-8" />
      <path d="M9 16.5l1 1 8-8" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3.5 2" />
    </>
  ),
  chats: (
    <>
      <path d="M21 11.5a8.4 8.4 0 0 1-8.5 8.5 8.6 8.6 0 0 1-3.8-.9L3 21l1.9-5.7A8.4 8.4 0 0 1 12.5 3 8.4 8.4 0 0 1 21 11.5z" />
    </>
  ),
  info: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5" />
      <circle cx="12" cy="7.8" r="0.9" fill="currentColor" stroke="none" />
    </>
  ),
  archive: (
    <>
      <rect x="3" y="4" width="18" height="4" rx="1" />
      <path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8" />
      <path d="M10 12h4" />
    </>
  ),
};

export function Icon({ name, size = 24 }: { name: IconName; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.9}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      focusable={false}
    >
      {PATHS[name]}
    </svg>
  );
}
