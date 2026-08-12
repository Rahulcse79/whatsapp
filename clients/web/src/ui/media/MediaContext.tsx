// MediaProvider builds one WebMediaService for the authed session and shares it
// through context, plus the hooks screens use to drive a single attachment
// (`useDownload`) or watch the whole transfer queue (`useDownloadQueue`).

import type { DownloadItem, MediaEnvelope } from "@wa/media-pipeline";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { config } from "../../config";
import { useServices } from "../ServicesContext";
import { WebMediaService, webDownloadTransport } from "./webMediaService";

const MediaContext = createContext<WebMediaService | null>(null);

export function MediaProvider({ children }: { children: ReactNode }) {
  const { services } = useServices();

  const service = useMemo(() => {
    const token = (): string => services.sessions.current()?.accessJwt ?? "";
    return new WebMediaService(webDownloadTransport(config.apiBaseUrl, token));
  }, [services]);

  useEffect(() => () => service.dispose(), [service]);

  return <MediaContext.Provider value={service}>{children}</MediaContext.Provider>;
}

export function useMediaService(): WebMediaService {
  const ctx = useContext(MediaContext);
  if (!ctx) throw new Error("useMediaService must be used inside <MediaProvider>");
  return ctx;
}

/** useDownload requests one attachment and tracks it, returning a ready-to-use
 *  `blob:` URL once decrypted. */
export function useDownload(env: MediaEnvelope): { item: DownloadItem; url: string | null; retry: () => void } {
  const svc = useMediaService();
  const [item, setItem] = useState<DownloadItem>(
    () => svc.manager.get(env.objectKey) ?? { objectKey: env.objectKey, state: "queued", attempts: 0 },
  );

  useEffect(() => {
    setItem(svc.request(env));
    return svc.subscribe((it) => {
      if (it.objectKey === env.objectKey) setItem(it);
    });
    // objectKey identifies the attachment; the envelope is otherwise stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [svc, env.objectKey]);

  const url = item.state === "ready" && item.bytes ? svc.objectUrl(env.objectKey, item.bytes, env.mime) : null;
  return { item, url, retry: () => svc.retry(env.objectKey) };
}

/** useDownloadQueue watches every tracked transfer (for the downloads panel). */
export function useDownloadQueue(): DownloadItem[] {
  const svc = useMediaService();
  const [items, setItems] = useState<DownloadItem[]>(() => svc.items());
  useEffect(() => {
    setItems(svc.items());
    return svc.subscribe(() => setItems(svc.items()));
  }, [svc]);
  return items;
}
