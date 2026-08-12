// Presentation metadata for a media attachment — pure helpers shared by both
// clients so "1.2 MB", "0:07", and the image/video/audio/document split render
// identically on web and mobile. No rendering here; just classification and
// formatting over a MediaEnvelope's advisory fields.

import type { MediaEnvelope } from "./envelope";

/** How the UI should render an attachment. `audio` covers both music files and
 *  voice notes; the composer flags voice notes separately (envelope.voice). */
export type MediaKind = "image" | "video" | "audio" | "document";

/** classifyMedia maps a MIME type to a render bucket. The claimed MIME is
 *  advisory (the recipient still owns the bytes), which is fine for choosing a
 *  presentation — never for a security decision. */
export function classifyMedia(mime: string): MediaKind {
  const m = mime.toLowerCase();
  if (m.startsWith("image/")) return "image";
  if (m.startsWith("video/")) return "video";
  if (m.startsWith("audio/")) return "audio";
  return "document";
}

/** isVoiceNote is true for an audio attachment the composer recorded in-app. */
export function isVoiceNote(env: MediaEnvelope): boolean {
  return env.voice === true && classifyMedia(env.mime) === "audio";
}

/** formatBytes renders a human size: "812 B", "1.2 MB", "3 GB". */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "0 B";
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]!}`;
}

/** formatDuration renders milliseconds as m:ss (or h:mm:ss past an hour). */
export function formatDuration(ms: number): string {
  const total = Number.isFinite(ms) ? Math.max(0, Math.round(ms / 1000)) : 0;
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const ss = String(s).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
}

const EXTENSIONS: Record<string, string> = {
  "image/jpeg": "jpg",
  "image/png": "png",
  "image/webp": "webp",
  "image/gif": "gif",
  "image/avif": "avif",
  "image/heic": "heic",
  "video/mp4": "mp4",
  "video/webm": "webm",
  "video/quicktime": "mov",
  "audio/ogg": "ogg",
  "audio/opus": "opus",
  "audio/mpeg": "mp3",
  "audio/mp4": "m4a",
  "audio/wav": "wav",
  "application/pdf": "pdf",
  "application/zip": "zip",
  "text/plain": "txt",
};

/** guessExtension returns a file extension for a MIME type (for save prompts). */
export function guessExtension(mime: string): string {
  const base = mime.toLowerCase().split(";")[0]!.trim();
  return EXTENSIONS[base] ?? "bin";
}

/** downloadName is the filename to save an attachment as: the sender-supplied
 *  name when present, otherwise a synthesized `media.<ext>`. */
export function downloadName(env: MediaEnvelope): string {
  const named = env.filename?.trim();
  if (named) return named;
  return `media.${guessExtension(env.mime)}`;
}
