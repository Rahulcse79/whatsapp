// Paints a BlurHash into a tiny canvas — the instant placeholder behind an
// image/video bubble while the real bytes download and decrypt. Decoding is
// shared (@wa/media-pipeline); here we just blit the RGBA into a 2D context and
// let CSS scale it up (the blur is inherent to the low-res hash).

import { decodeBlurhash } from "@wa/media-pipeline";
import { useEffect, useRef } from "react";

const W = 32;
const H = 32;

export function BlurhashCanvas({ hash, className }: { hash: string; className?: string }) {
  const ref = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    try {
      const pixels = decodeBlurhash(hash, W, H);
      ctx.putImageData(new ImageData(pixels, W, H), 0, 0);
    } catch {
      // Invalid hash → leave the canvas transparent; the CSS avg-colour shows.
    }
  }, [hash]);

  return <canvas ref={ref} width={W} height={H} className={className} aria-hidden />;
}
