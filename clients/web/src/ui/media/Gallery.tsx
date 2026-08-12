// Gallery — the full-screen lightbox for image/video attachments. Given the
// visual attachments in a thread and the one that was tapped, it shows it large
// with prev/next paging and keyboard navigation (←/→/Esc). Each slide reuses the
// shared download manager, so opening the gallery costs nothing once the bubble
// has already fetched the bytes.

import { classifyMedia, downloadName, type MediaEnvelope } from "@wa/media-pipeline";
import { useCallback, useEffect, useState } from "react";
import { useDownload } from "./MediaContext";

export function Gallery({ items, startKey, onClose }: { items: MediaEnvelope[]; startKey: string; onClose: () => void }) {
  const [index, setIndex] = useState(() => Math.max(0, items.findIndex((e) => e.objectKey === startKey)));

  const go = useCallback(
    (delta: number) => setIndex((i) => (i + delta + items.length) % items.length),
    [items.length],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowRight") go(1);
      else if (e.key === "ArrowLeft") go(-1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [go, onClose]);

  const current = items[index];
  if (!current) return null;

  return (
    <div className="gallery" role="dialog" aria-modal onClick={onClose}>
      <button type="button" className="gallery-close" onClick={onClose} aria-label="Close">
        ✕
      </button>
      {items.length > 1 ? (
        <>
          <button
            type="button"
            className="gallery-nav left"
            onClick={(e) => {
              e.stopPropagation();
              go(-1);
            }}
            aria-label="Previous"
          >
            ‹
          </button>
          <button
            type="button"
            className="gallery-nav right"
            onClick={(e) => {
              e.stopPropagation();
              go(1);
            }}
            aria-label="Next"
          >
            ›
          </button>
        </>
      ) : null}
      <div className="gallery-stage" onClick={(e) => e.stopPropagation()}>
        <Slide env={current} />
      </div>
      <div className="gallery-count">
        {index + 1} / {items.length}
      </div>
    </div>
  );
}

function Slide({ env }: { env: MediaEnvelope }) {
  const { item, url, retry } = useDownload(env);

  if (item.state === "error") {
    return (
      <button type="button" className="btn" onClick={retry}>
        ⟳ Retry download
      </button>
    );
  }
  if (!url) return <span className="spinner large" />;

  return classifyMedia(env.mime) === "video" ? (
    <video className="gallery-media" src={url} controls autoPlay />
  ) : (
    <img className="gallery-media" src={url} alt={downloadName(env)} />
  );
}
