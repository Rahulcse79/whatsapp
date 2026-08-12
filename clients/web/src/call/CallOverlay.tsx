// CallOverlay renders the active-call surface driven by the CallSession phase:
// an incoming ring (accept/decline), an outgoing ring, the connecting/connected
// in-call bar, and a brief ended notice. Hidden when idle.

import { active, type CallState } from "@wa/call-engine";
import { useEffect, useState } from "react";
import { useCall } from "./CallContext";

export function CallOverlay() {
  const { state, accept, decline, hangup } = useCall();

  if (state.phase === "idle") return null;
  if (state.phase === "ended") return <EndedNotice state={state} />;

  return (
    <div className="call-overlay">
      <div className="call-card">
        <div className="call-peer mono">{state.peerId?.slice(0, 12) ?? "unknown"}</div>
        <div className="call-status">{statusLabel(state)}</div>
        <div className="call-actions">
          {state.phase === "incoming" ? (
            <>
              <button className="btn danger" onClick={decline}>
                Decline
              </button>
              <button className="btn" onClick={accept}>
                Accept
              </button>
            </>
          ) : (
            <button className="btn danger" onClick={hangup}>
              {active(state) ? "End" : "Cancel"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function statusLabel(state: CallState): string {
  switch (state.phase) {
    case "outgoing":
      return "Ringing…";
    case "incoming":
      return `Incoming ${state.kind ?? "voice"} call`;
    case "connecting":
      return "Connecting…";
    case "connected":
      return "Connected";
    default:
      return "";
  }
}

/** EndedNotice shows the end reason briefly, then clears itself. */
function EndedNotice({ state }: { state: CallState }) {
  const [visible, setVisible] = useState(true);
  useEffect(() => {
    const t = setTimeout(() => setVisible(false), 2500);
    return () => clearTimeout(t);
  }, []);
  if (!visible) return null;
  return (
    <div className="call-overlay">
      <div className="call-card">
        <div className="call-status">Call ended{state.endReason ? ` · ${state.endReason}` : ""}</div>
      </div>
    </div>
  );
}
