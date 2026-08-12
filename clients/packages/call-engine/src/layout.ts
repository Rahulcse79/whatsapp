// Group-call layout + subscription policy (HLD §10.4). The active speaker (or a
// pinned participant) becomes the focused/large tile; the rest fill a grid capped
// at maxTiles; anyone beyond the cap stays audio-only. The chosen layout maps —
// via chooseReceiveLayer (T2.05) — to a per-participant receive layer, which is
// what drives the SFU to switch each subscription as the active speaker changes.

import { chooseReceiveLayer, type LayerId } from "./simulcast";

export interface Tile {
  participantId: string;
  focused: boolean;
}

export interface GroupLayout {
  tiles: Tile[];
}

/**
 * computeLayout arranges up to maxTiles participants. A pinned participant, else
 * the active speaker, is the focused tile and sorts first; the rest follow in
 * roster order. Participants past maxTiles get no video tile (audio-only),
 * capping decode and downlink cost.
 */
export function computeLayout(
  participants: string[],
  activeSpeaker: string | null,
  maxTiles: number,
  pinned?: string | null,
): GroupLayout {
  const focusId =
    pinned && participants.includes(pinned)
      ? pinned
      : activeSpeaker && participants.includes(activeSpeaker)
        ? activeSpeaker
        : null;

  const ordered = focusId ? [focusId, ...participants.filter((p) => p !== focusId)] : [...participants];
  const shown = ordered.slice(0, Math.max(1, maxTiles));
  return { tiles: shown.map((p) => ({ participantId: p, focused: p === focusId })) };
}

/**
 * desiredReceiveLayers maps a layout + the receiver's downlink to a per-tile
 * receive layer (chooseReceiveLayer): the focused tile gets the high layer, the
 * rest reduced layers, and any participant not in the layout is audio-only. This
 * is the subscription switch the SFU applies as the layout changes.
 */
export function desiredReceiveLayers(
  allParticipants: string[],
  layout: GroupLayout,
  downlinkKbps: number,
): Map<string, LayerId | "audio-only"> {
  const shown = new Map(layout.tiles.map((t) => [t.participantId, t.focused]));
  const tileCount = layout.tiles.length;
  const out = new Map<string, LayerId | "audio-only">();
  for (const p of allParticipants) {
    const focused = shown.get(p);
    if (focused === undefined) {
      out.set(p, "audio-only");
      continue;
    }
    out.set(p, chooseReceiveLayer({ downlinkKbps, tileCount, focused }));
  }
  return out;
}
