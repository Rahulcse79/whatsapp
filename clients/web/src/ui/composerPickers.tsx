// Composer pickers for the rich composer (T6.01): a curated emoji grid, the
// server-proxied GIF search, and the sticker-pack browser. Each renders as a
// small popover above the composer; the parent positions/toggles them.

import { useEffect, useState } from "react";
import type { GifResult, StickerItem, StickerPackInfo } from "../services/appServices";
import { useServices } from "./ServicesContext";

// A compact, dependency-free emoji set spanning the common categories.
const EMOJI = [
  "😀", "😂", "🙂", "😉", "😍", "😘", "😎", "🤔", "😴", "😭", "😡", "🥳",
  "👍", "👎", "👏", "🙏", "💪", "🔥", "🎉", "✅", "❌", "⭐", "💯", "👀",
  "❤️", "🧡", "💛", "💚", "💙", "💜", "🖤", "💔", "😅", "😇", "🤗", "🤝",
  "🍕", "🍔", "☕", "🍺", "🎂", "⚽", "🏀", "🚀", "🌈", "☀️", "🌙", "💩",
];

export function EmojiPicker({ onPick, onClose }: { onPick: (emoji: string) => void; onClose: () => void }) {
  return (
    <div className="picker-pop" role="dialog" aria-label="Emoji">
      <div className="picker-head">
        <span>Emoji</span>
        <button className="btn small ghost" aria-label="Close" onClick={onClose}>×</button>
      </div>
      <div className="emoji-grid">
        {EMOJI.map((e) => (
          <button key={e} className="emoji-cell" aria-label={e} onClick={() => onPick(e)}>
            {e}
          </button>
        ))}
      </div>
    </div>
  );
}

export function GifPicker({ onPick, onClose }: { onPick: (gif: GifResult) => void; onClose: () => void }) {
  const { services } = useServices();
  const [q, setQ] = useState("");
  const [results, setResults] = useState<GifResult[]>([]);
  const [state, setState] = useState<"idle" | "loading" | "empty">("idle");

  useEffect(() => {
    const term = q.trim();
    if (!term) {
      setResults([]);
      setState("idle");
      return;
    }
    setState("loading");
    const handle = setTimeout(() => {
      void services.searchGifs(term).then((r) => {
        setResults(r);
        setState(r.length === 0 ? "empty" : "idle");
      });
    }, 350); // debounce
    return () => clearTimeout(handle);
  }, [q, services]);

  return (
    <div className="picker-pop" role="dialog" aria-label="GIF search">
      <div className="picker-head">
        <input className="input" placeholder="Search GIFs…" value={q} autoFocus onChange={(e) => setQ(e.target.value)} />
        <button className="btn small ghost" aria-label="Close" onClick={onClose}>×</button>
      </div>
      {state === "loading" ? <p className="muted" style={{ margin: 8 }}>Searching…</p> : null}
      {state === "empty" ? (
        <p className="muted" style={{ margin: 8 }}>No GIFs (the GIF proxy may be disabled on this server).</p>
      ) : null}
      <div className="gif-grid">
        {results.map((g) => (
          <button key={g.id} className="gif-cell" onClick={() => onPick(g)} aria-label="Send GIF">
            <img src={g.previewUrl} alt="" loading="lazy" />
          </button>
        ))}
      </div>
    </div>
  );
}

export function StickerPicker({ onPick, onClose }: { onPick: (s: StickerItem) => void; onClose: () => void }) {
  const { services } = useServices();
  const [packs, setPacks] = useState<StickerPackInfo[]>([]);
  const [active, setActive] = useState<StickerPackInfo | null>(null);

  useEffect(() => {
    void services.stickerPacks().then(async (list) => {
      setPacks(list);
      if (list[0]) {
        const full = await services.stickerPack(list[0].id);
        setActive(full ?? list[0]);
      }
    });
  }, [services]);

  const selectPack = (id: string): void => {
    void services.stickerPack(id).then((p) => p && setActive(p));
  };

  return (
    <div className="picker-pop" role="dialog" aria-label="Stickers">
      <div className="picker-head">
        <span>Stickers</span>
        <button className="btn small ghost" aria-label="Close" onClick={onClose}>×</button>
      </div>
      {packs.length === 0 ? (
        <p className="muted" style={{ margin: 8 }}>No sticker packs available.</p>
      ) : (
        <>
          <div className="sticker-tabs">
            {packs.map((p) => (
              <button key={p.id} className={`btn small ghost${active?.id === p.id ? " on" : ""}`} onClick={() => selectPack(p.id)}>
                {p.title}
              </button>
            ))}
          </div>
          <div className="sticker-grid">
            {(active?.stickers ?? []).map((s) => (
              <button key={s.id} className="sticker-cell" onClick={() => onPick(s)} aria-label={`Send sticker ${s.emoji}`}>
                {s.emoji}
              </button>
            ))}
            {active && (active.stickers ?? []).length === 0 ? <p className="muted" style={{ margin: 8 }}>This pack has no stickers yet.</p> : null}
          </div>
        </>
      )}
    </div>
  );
}
