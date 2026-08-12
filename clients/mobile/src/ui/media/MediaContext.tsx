// MediaProvider builds one RnMediaService for the authed session and shares it,
// plus the hooks screens use to drive one attachment (useDownload) or watch the
// whole queue (useDownloadQueue). Mirrors the web client's context so the two
// UIs read the same.

import type { DownloadItem, MediaEnvelope } from "@wa/media-pipeline";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { defaultConfig } from "../../services/appServices";
import { useServices } from "../ServicesContext";
import { RnMediaService, rnDownloadTransport, type RnMediaHandlers } from "./rnMediaService";

const MediaContext = createContext<RnMediaService | null>(null);

export function MediaProvider({ children, handlers }: { children: ReactNode; handlers?: RnMediaHandlers }) {
  const { services } = useServices();

  const service = useMemo(() => {
    const token = (): string => services.sessions.current()?.accessJwt ?? "";
    return new RnMediaService(rnDownloadTransport(defaultConfig.apiBaseUrl, token), handlers);
  }, [services, handlers]);

  useEffect(() => () => service.dispose(), [service]);

  return <MediaContext.Provider value={service}>{children}</MediaContext.Provider>;
}

export function useMediaService(): RnMediaService {
  const ctx = useContext(MediaContext);
  if (!ctx) throw new Error("useMediaService must be used inside <MediaProvider>");
  return ctx;
}

export function useDownload(env: MediaEnvelope): { item: DownloadItem; uri: string | null; retry: () => void } {
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
  }, [svc, env.objectKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const uri = item.state === "ready" && item.bytes ? svc.dataUri(env.objectKey, item.bytes, env.mime) : null;
  return { item, uri, retry: () => svc.retry(env.objectKey) };
}

export function useDownloadQueue(): DownloadItem[] {
  const svc = useMediaService();
  const [items, setItems] = useState<DownloadItem[]>(() => svc.items());
  useEffect(() => {
    setItems(svc.items());
    return svc.subscribe(() => setItems(svc.items()));
  }, [svc]);
  return items;
}
