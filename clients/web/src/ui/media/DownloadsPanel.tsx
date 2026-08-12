// DownloadsPanel — the "download manager" surface. It lists in-flight, queued,
// and failed transfers with a retry affordance, and hides itself when nothing is
// active. Ready downloads drop off the list (they're shown inline in the bubble).

import { useDownloadQueue, useMediaService } from "./MediaContext";

export function DownloadsPanel() {
  const svc = useMediaService();
  const items = useDownloadQueue().filter((i) => i.state !== "ready");
  if (items.length === 0) return null;

  const active = items.filter((i) => i.state === "downloading" || i.state === "queued").length;
  const failed = items.filter((i) => i.state === "error").length;

  return (
    <div className="downloads">
      <div className="downloads-head">
        <span>Transfers</span>
        <span className="muted">
          {active > 0 ? `${active} active` : null}
          {active > 0 && failed > 0 ? " · " : null}
          {failed > 0 ? `${failed} failed` : null}
        </span>
      </div>
      <ul className="downloads-list">
        {items.map((i) => (
          <li key={i.objectKey} className="downloads-row">
            <span className="mono ellipsis">{i.objectKey.split("/").pop() ?? i.objectKey}</span>
            {i.state === "error" ? (
              <button type="button" className="btn small ghost" onClick={() => svc.retry(i.objectKey)}>
                ⟳ Retry
              </button>
            ) : (
              <span className="spinner tiny" />
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
