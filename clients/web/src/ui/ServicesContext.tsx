import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { AppServices } from "../services/appServices";

interface ServicesValue {
  services: AppServices;
  authed: boolean;
  setAuthed: (v: boolean) => void;
}

const ServicesContext = createContext<ServicesValue | null>(null);

/** Boots AppServices once (opening the DB/crypto worker) and gates the tree. */
export function ServicesProvider({ children }: { children: ReactNode }) {
  const [services, setServices] = useState<AppServices | null>(null);
  const [authed, setAuthed] = useState(false);

  useEffect(() => {
    let alive = true;
    AppServices.create()
      .then((s) => {
        if (!alive) return;
        setServices(s);
        if (s.hasSession()) {
          s.startRealtime();
          setAuthed(true);
        }
      })
      .catch(() => {
        /* boot failure surfaces as a stuck splash; a crash reporter covers it later */
      });
    return () => {
      alive = false;
    };
  }, []);

  if (!services) return <div className="splash">Loading…</div>;
  return <ServicesContext.Provider value={{ services, authed, setAuthed }}>{children}</ServicesContext.Provider>;
}

export function useServices(): ServicesValue {
  const ctx = useContext(ServicesContext);
  if (!ctx) throw new Error("useServices must be used inside <ServicesProvider>");
  return ctx;
}
