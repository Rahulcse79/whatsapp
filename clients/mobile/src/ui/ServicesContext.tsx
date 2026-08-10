import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { AppServices } from "../services/appServices";

interface ServicesValue {
  services: AppServices;
  authed: boolean;
  setAuthed: (v: boolean) => void;
}

const ServicesContext = createContext<ServicesValue | null>(null);

/** ServicesProvider boots AppServices once and gates the tree until it is ready. */
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

  if (!services) {
    return (
      <View style={styles.splash}>
        <ActivityIndicator size="large" />
      </View>
    );
  }

  return <ServicesContext.Provider value={{ services, authed, setAuthed }}>{children}</ServicesContext.Provider>;
}

export function useServices(): ServicesValue {
  const ctx = useContext(ServicesContext);
  if (!ctx) throw new Error("useServices must be used inside <ServicesProvider>");
  return ctx;
}

const styles = StyleSheet.create({
  splash: { flex: 1, alignItems: "center", justifyContent: "center" },
});
