// MediaMessage renders one attachment inside a chat bubble, dispatching on the
// media kind: image/video → tappable thumbnail (blurhash placeholder → decrypted
// media, opens the gallery), audio → voice-note player, document → a file tile
// with a download affordance. All transfer state (spinner, error, retry) comes
// from the shared DownloadManager via useDownload.

import { blurhashCssColor, classifyMedia, downloadName, formatBytes, formatDuration, isVoiceNote, type MediaEnvelope } from "@wa/media-pipeline";
import { useDownload } from "./MediaContext";
import { BlurhashCanvas } from "./BlurhashCanvas";

export function MediaMessage({ env, onOpen }: { env: MediaEnvelope; onOpen?: (env: MediaEnvelope) => void }) {
  switch (classifyMedia(env.mime)) {
    case "image":
      return <VisualBubble env={env} onOpen={onOpen} kind="image" />;
    case "video":
      return <VisualBubble env={env} onOpen={onOpen} kind="video" />;
    case "audio":
      return <VoiceNote env={env} />;
    default:
      return <DocumentTile env={env} />;
  }
}

function VisualBubble({ env, onOpen, kind }: { env: MediaEnvelope; onOpen?: (env: MediaEnvelope) => void; kind: "image" | "video" }) {
  const { item, url, retry } = useDownload(env);
  const ratio = env.width && env.height ? env.width / env.height : 4 / 3;

  return (
    <button
      type="button"
      className="media-visual"
      style={{ aspectRatio: String(ratio), background: blurhashCssColor(env.blurhash) }}
      onClick={() => url && onOpen?.(env)}
      aria-label={kind === "video" ? "Play video" : "Open image"}
    >
      {env.blurhash ? <BlurhashCanvas hash={env.blurhash} className="media-blur" /> : null}
      {url && kind === "image" ? <img className="media-full" src={url} alt="" /> : null}
      {url && kind === "video" ? (
        <>
          <video className="media-full" src={url} muted preload="metadata" />
          <span className="media-play">▶</span>
        </>
      ) : null}
      {item.state !== "ready" ? (
        <span className="media-overlay">
          {item.state === "error" ? (
            <span
              role="button"
              tabIndex={0}
              className="media-retry"
              onClick={(e) => {
                e.stopPropagation();
                retry();
              }}
            >
              ⟳ Retry
            </span>
          ) : (
            <span className="spinner" />
          )}
        </span>
      ) : null}
    </button>
  );
}

function VoiceNote({ env }: { env: MediaEnvelope }) {
  const { item, url, retry } = useDownload(env);
  const voice = isVoiceNote(env);

  return (
    <div className={`voice-note ${voice ? "is-voice" : ""}`}>
      {url ? (
        // Native controls handle play/scrub; the duration hint comes from the envelope.
        <audio className="voice-audio" src={url} controls preload="metadata" />
      ) : item.state === "error" ? (
        <button type="button" className="btn small ghost" onClick={retry}>
          ⟳ Retry
        </button>
      ) : (
        <span className="voice-loading">
          <span className="spinner" /> {voice ? "Voice message" : "Audio"}
        </span>
      )}
      {env.durationMs ? <span className="voice-time">{formatDuration(env.durationMs)}</span> : null}
    </div>
  );
}

function DocumentTile({ env }: { env: MediaEnvelope }) {
  const { item, url, retry } = useDownload(env);
  const name = downloadName(env);

  return (
    <div className="doc-tile">
      <span className="doc-icon" aria-hidden>
        📄
      </span>
      <span className="doc-meta">
        <span className="doc-name" title={name}>
          {name}
        </span>
        <span className="doc-sub">{formatBytes(env.sizeBytes)}</span>
      </span>
      {url ? (
        <a className="btn small" href={url} download={name}>
          Save
        </a>
      ) : item.state === "error" ? (
        <button type="button" className="btn small ghost" onClick={retry}>
          ⟳
        </button>
      ) : (
        <span className="spinner" />
      )}
    </div>
  );
}
