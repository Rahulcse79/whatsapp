# UI pass 2 — WhatsApp fidelity

A second UI pass, after the design-system work in
[UI-MODERNIZATION.md](UI-MODERNIZATION.md). That pass fixed *consistency* —
tokens, a screen shell, primitives, dark mode. This one fixes **fidelity**: the
places where the app is internally consistent but still doesn't read like
WhatsApp.

No screenshots were supplied with the request, so the audit below is from the
running app captured directly (light and dark, desktop), not from description.

---

## Audit — what actually differs from WhatsApp

Captured on a chat with four sent messages.

### The message surface (worst offender)

| # | Gap | Why it matters |
|---|-----|----------------|
| M1 | **No date separators.** WhatsApp puts a centered `TODAY` / `YESTERDAY` / date pill above the first message of each day. We render one flat list. | The single most recognisable element of a WhatsApp thread, and the only way to read time context when scrolling back. |
| M2 | **No message grouping.** Consecutive messages from the same sender should tighten (small gap) and share one tail. We give every bubble the same gap. | Runs of messages look like separate conversations. |
| M3 | **A tail on every bubble.** WhatsApp draws the tail only on the *first* bubble of a run. | Repeated tails are visually noisy and immediately read as "not WhatsApp". |
| M4 | **Timestamp/tick cramped.** The meta should float bottom-right *inside* the bubble with the text wrapping around it, reserving space so it never collides. Ours overlaps on wrapped text. | Visible collision on the multi-line bubble in the capture. |
| M5 | **No unread divider.** WhatsApp shows a full-width `N unread messages` marker at the read boundary. | No way to see where you left off. |

### The chat list

| # | Gap |
|---|-----|
| L1 | **No filter chips.** WhatsApp has `All / Unread / Favourites / Groups` under the search field. Absent entirely, though the underlying state (unread counts, favourites, group detection) already exists. |
| L2 | **No delivery tick in the row preview.** When the last message is your own, WhatsApp prefixes the preview with its ✓/✓✓ state. |

### Iconography

| # | Gap |
|---|-----|
| I1 | Several rail icons are wrong or unrecognisable — Communities in particular renders as an ambiguous glyph. Status/Channels don't match WhatsApp's shapes either. |

### Chrome

| # | Gap |
|---|-----|
| C1 | Chat wallpaper reads pinkish and the doodle pattern is nearly invisible in light mode. |
| C2 | Thread header shows only a name; WhatsApp shows a presence/last-seen line under it and falls back to the phone number. |

### Already correct — do not redo

Design tokens, the `Screen`/`Section` shell, buttons/inputs/switches, empty
states, focus rings, dark-mode token parity, the mobile tab bar, and the
composer's structure all landed in pass 1 and hold up against WhatsApp.

---

## Phases

- [x] **Phase 1 — Message surface.** *(M1–M4 done and verified in the DOM; M5's styling is in place but the unread divider is not yet wired to a read watermark.)* Date separators (M1), sender/time grouping
  with a single tail per run (M2, M3), correct meta float so time+ticks never
  collide with wrapped text (M4), and the unread divider (M5).
- [x] **Phase 2 — Chat list.** Filter chips wired to the existing unread /
  favourite / group state (L1). **L2 deferred:** the delivery tick in the row
  preview needs `ChatSummary` to carry the last message's `mine`/`state`, which
  means changing the shared type and both repos in `@wa/client-core` — the
  package the parallel session is mid-extraction on. Not worth the collision for
  a tick in a preview.
- [x] **Phase 3 — Icons.** Replace the inaccurate rail and action icons with
  shapes that match WhatsApp (I1).
- [x] **Phase 4 — Chrome / responsive.** Fixed a real horizontal-overflow bug at
  phone widths (below). C1/C2 reassessed as not worth changing: the wallpaper
  token is already WhatsApp's `#efeae2`, and the header presence line already
  renders whenever presence is known — it was blank only because the dev peer
  has no profile.
- [x] **Phase 5 — Verification.** Measured rather than eyeballed; see below.

---

## Responsive bug found and fixed

At a 375px viewport the document overflowed horizontally — `clientWidth` 375
against `scrollWidth` 434, i.e. a 59px sideways scroll on every screen. Walking
the DOM for elements extending past the viewport traced it to `.pane-head`: the
thread header carries back, avatar, title and four action icons, and a flex row
will not shrink below its content by default, so the header forced `.pane` and
`.wa-detail` wider than the phone, and the page with them.

Three fixes, smallest first:

- `min-width: 0` on `.pane-head` and its children so the row may shrink, with
  the icons and avatar pinned via `flex-shrink: 0` — the title gives way, not
  the controls.
- Below 480px the in-chat search icon is dropped from the header. That matches
  WhatsApp, whose mobile chat header carries video, voice and the overflow menu
  only.
- `overflow-x: hidden` on `html, body` as a backstop, so one future overflowing
  child cannot reintroduce a whole-app sideways scroll.

Verified after: `scrollWidth === clientWidth` (375/375), no overflow, and the
search icon correctly absent at phone width.
