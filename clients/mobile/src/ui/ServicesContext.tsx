import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { AppServices } from "../services/appServices";
import { deriveConfig, loadServerUrl, saveServerUrl } from "../services/serverConfig";
import { ServerSetup } from "./ServerSetup";

interface ServicesValue {
  services: AppServices;
  authed: boolean;
  setAuthed: (v: boolean) => void;
  serverUrl: string;
  editServer: () => void; // reopen the server-address screen
}

const ServicesContext = createContext<ServicesValue | null>(null);

/**
 * ServicesProvider first ensures a server address is chosen (persisted
 * on-device), then boots AppServices against it and gates the tree until ready.
 * The address can be changed later via editServer(), which re-boots against the
 * new backend.
 */
export function ServicesProvider({ children }: { children: ReactNode }) {
  const [serverUrl, setServerUrl] = useState<string | null | undefined>(undefined); // undefined = still loading
  const [editing, setEditing] = useState(false);
  const [services, setServices] = useState<AppServices | null>(null);
  const [authed, setAuthed] = useState(false);

  // Load the saved address once.
  useEffect(() => {
    loadServerUrl().then(setServerUrl);
  }, []);

  // (Re)boot services whenever there's an address and no live services yet.
  useEffect(() => {
    if (!serverUrl || services) return;
    let alive = true;
    AppServices.create(deriveConfig(serverUrl))
      .then((s) => {
        if (!alive) return;
        setServices(s);
        if (s.hasSession()) {
          s.startRealtime();
          setAuthed(true);
        }
      })
      .catch(() => {
        /* boot failure surfaces as a stuck splash; the user can edit the server */
      });
    return () => {
      alive = false;
    };
  }, [serverUrl, services]);

  const handleSaved = useCallback((raw: string) => {
    void saveServerUrl(raw).finally(() => {
      setEditing(false);
      setAuthed(false);
      setServices(null); // drop the old backend's services → the effect re-boots
      setServerUrl(raw);
    });
  }, []);

  const editServer = useCallback(() => setEditing(true), []);

  if (serverUrl === undefined) {
    return <Splash />;
  }
  if (!serverUrl || editing) {
    return (
      <ServerSetup
        initial={serverUrl ?? ""}
        onSaved={handleSaved}
        onCancel={editing && serverUrl ? () => setEditing(false) : undefined}
      />
    );
  }
  if (!services) {
    return <Splash />;
  }

  return (
    <ServicesContext.Provider value={{ services, authed, setAuthed, serverUrl, editServer }}>
      {children}
    </ServicesContext.Provider>
  );
}

function Splash() {
  return (
    <View style={styles.splash}>
      <ActivityIndicator size="large" />
    </View>
  );
}

export function useServices(): ServicesValue {
  const ctx = useContext(ServicesContext);
  if (!ctx) throw new Error("useServices must be used inside <ServicesProvider>");
  return ctx;
}

const styles = StyleSheet.create({
  splash: { flex: 1, alignItems: "center", justifyContent: "center" },
});
