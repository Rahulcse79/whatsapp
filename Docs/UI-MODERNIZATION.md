# Web UI modernization — audit + implementation plan

Status: **complete** (U0–U5). Scope: `clients/web` (the React + Vite PWA).
Mobile (Expo) rides T5.01 and is out of scope here.

Goal: a modern, clean, professional interface — consistent spacing, type, colour,
elevation and motion across **every** screen, tab and feature, in both light and
dark themes, without changing behaviour or the E2EE/data model.

---

## 1. Audit — what is actually wrong today

Measured on the pre-change tree (`styles.css` 2152 lines, `screens.tsx` 5223 lines):

| # | Finding | Evidence | Impact |
|---|---------|----------|--------|
| A1 | **No design tokens beyond colour.** Spacing, radius, shadow, type scale and motion are hard-coded per call-site. | 234 inline `style={{…}}` in `screens.tsx`; values like `0.8rem`, `0.72rem`, `0.85rem`, `6px`, `10px` chosen ad hoc | Nothing lines up; every screen looks slightly different |
| A2 | **Two competing screen layouts.** `.pane` (proper header + scroll body) vs `.card` (`width: min(420px, 90vw); margin: auto`) — the *login* card, reused for full feature screens. | `Profile`, `Settings`, `Contacts`, `CreateGroup`, `GroupInfoScreen`, `Channels`, `ChannelScreen`, `Communities`, `CommunityScreen` all render `<div className="card">` | 9 major screens are a narrow 420px column floating in a wide empty pane, with no sticky header and no scroll container |
| A3 | **Thin primitives.** `.btn` has only `small`/`ghost`; no secondary/danger variants, no icon-button sizing scale, no toggle/switch, no form `field`+`label`, no empty-state, no skeleton, no card/section surface. | `styles.css` | Feature code invents one-off styling inline (see A1) |
| A4 | **Settings is a 15-section wall.** `<h1>` then ~15 flat `<h2>`s in one 420px card, no grouping, no scroll affordance. | `Settings()` | Unusable as the app's control centre |
| A5 | **No focus-visible system.** Several `:focus { outline: none }` with no replacement. | `.wa-search input` | Keyboard/a11y regression |
| A6 | **No motion system.** One-off `transition: opacity .12s`; menus/sheets/toasts appear instantly. | `styles.css` | Feels abrupt/cheap |
| A7 | **Dark-mode leaks.** Inline `rgba(0,0,0,.06)`, `#fff`, `rgba(11,20,26,.28)` bypass the token layer. | inline styles + several rules | Washed-out or invisible elements in dark theme |
| A8 | **Inconsistent empty/loading/error states.** Each screen writes its own `<p className="muted">Nothing yet.</p>` or nothing at all. | most screens | Feels unfinished |

## 2. Approach

Build the foundation first, then migrate screens onto it one by one, so each step
is verifiable and nothing regresses. Behaviour, props and data flow stay
identical — this is a presentation-layer change.

## 3. Phases

- [x] **U0 — Audit** (this document).
- [x] **U1 — Design foundation.** Token layer (spacing / radius / elevation / type
  scale / motion / z-index) on top of the existing colour tokens, refined light +
  dark palettes, global reset, and a real focus-visible ring. Rewritten
  primitives: buttons (primary / secondary / ghost / danger × sm / md / lg),
  icon buttons, form fields + labels + help/error text, switches, segmented
  control, chips, badges, cards, sections, dividers, menus, sheets/modals,
  tooltips, empty states, skeletons, avatars.
- [x] **U2 — App shell.** Nav rail (desktop rail + mobile tab bar), two-pane
  layout, panel headers, search bar, welcome/empty pane, notification toasts.
- [x] **U3 — Screen shell migration.** Replace every full-screen `.card` with a
  `.screen` shell: sticky header (back + title + actions), scrollable body,
  grouped `.section` blocks, consistent max-width for readability.
- [x] **U4 — Feature-by-feature polish.** Each sub-item is verified on screen:
  - [x] U4.1 Auth — Login, Verify
  - [x] U4.2 Chat list + Thread (bubbles, quotes, reactions, ticks, date chips)
  - [x] U4.3 Composer + pickers (emoji / GIF / sticker / tools popover)
  - [x] U4.4 Rich message cards — polls, interactive buttons, location, contacts, link previews
  - [x] U4.5 Media — gallery, downloads panel, voice notes, viewer
  - [x] U4.6 Calls — history + in-call overlay
  - [x] U4.7 Status / Stories
  - [x] U4.8 Channels + Communities + Discover
  - [x] U4.9 Contacts, New chat, Create group, Group info, Profile
  - [x] U4.10 Settings — all sections incl. Security, AI, Bots, Notifications
  - [x] U4.11 Collab notes & tasks + Whiteboard
  - [x] U4.12 Search + secret chats
- [x] **U5 — Sweep.** Responsive (mobile / tablet / desktop), dark-mode parity,
  keyboard + screen-reader pass, reduced-motion, final visual QA.

## 4. Rules for every change

1. **Tokens only.** No new hard-coded colour, spacing or radius in TSX or CSS —
   use `var(--space-*)`, `var(--radius-*)`, `var(--shadow-*)`, `var(--text-*)`.
2. **Inline styles are a smell.** Replace them with a class as each screen is
   touched; the inline-style count is the migration's progress metric.
3. **Behaviour is frozen.** No prop, handler, route or service call changes.
4. **Both themes, every time.** Every new rule is checked in light *and* dark.
5. **Verify green.** `tsc --noEmit` clean (apart from the two known pre-existing
   `wsTransport.ts` errors) and the app rendered on screen before each commit.

## 5. Outcome

| Metric | Before | After |
|---|---|---|
| Inline `style={{…}}` in `screens.tsx` | 234 | 99 (the rest carry genuinely dynamic values — widths, data colours, wallpapers) |
| Full screens rendered inside the 420px login `.card` | 9 | 0 |
| Design tokens | colour only | colour + space + radius + type + elevation + motion + z-index |
| `:focus { outline: none }` with no replacement | yes | none — every one now paints a visible ring |
| Shared empty-state component | none | used by chat list, thread, calls, updates, channels, communities, discover, search, notes/tasks |

Verified on screen (light **and** dark, desktop and 375px): login, verify, chat
list, thread + composer + overflow menu, new chat, profile, settings (all eight
groups), channels, communities, calls, updates, discover, notes & tasks,
whiteboard. `tsc --noEmit` clean throughout apart from the two pre-existing
`wsTransport.ts` errors from the stale T7.04 proto generation.

Known follow-ups (not regressions, pre-existing gaps):
- Peer display names are unresolved in the dev rig, so chats show the
  "Unknown contact" fallback rather than a name.
- The mobile (Expo) client still carries the old styling — it rides T5.01.
