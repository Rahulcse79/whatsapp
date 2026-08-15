// CallOverlay renders the active-call surface driven by the CallSession phase:
// an incoming ring (accept/decline), an outgoing ring, the connecting/connected
// in-call bar, and a brief ended notice. Hidden when idle.

import { active, type CallState } from "@wa/call-engine";
import { useEffect, useRef, useState } from "react";
import { useCall } from "./CallContext";

export function CallOverlay() {
  const { state, camera, screen, effect, localVideo, accept, decline, hangup, toggleCamera, flipCamera, toggleScreenShare, toggleBlur, setBackground, clearEffect } =
    useCall();

  if (state.phase === "idle") return null;
  if (state.phase === "ended") return <EndedNotice state={state} />;

  const isVideo = state.kind === "video";
  const inCall = state.phase === "connecting" || state.phase === "connected";

  return (
    <div className="call-overlay">
      <div className="call-card">
        {isVideo && camera.enabled ? <LocalPreview track={localVideo()} /> : null}
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
            <>
              {isVideo && inCall ? (
                <>
                  <button className="btn ghost" onClick={() => void toggleCamera()}>
                    {camera.enabled ? "Camera off" : "Camera on"}
                  </button>
                  {camera.enabled ? (
                    <>
                      <button className="btn ghost" onClick={() => void flipCamera()}>
                        Flip
                      </button>
                      <button className="btn ghost" onClick={() => void toggleBlur()}>
                        {effect.effect === "blur" ? "Unblur" : "Blur"}
                      </button>
                      <button
                        className="btn ghost"
                        onClick={() =>
                          effect.effect === "background"
                            ? void clearEffect()
                            : void setBackground("preset:office")
                        }
                      >
                        {effect.effect === "background" ? "No background" : "Background"}
                      </button>
                    </>
                  ) : null}
                </>
              ) : null}
              {inCall ? (
                <button className="btn ghost" onClick={() => void toggleScreenShare()}>
                  {screen.sharing ? "Stop share" : "Share screen"}
                </button>
              ) : null}
              <button className="btn danger" onClick={hangup}>
                {active(state) ? "End" : "Cancel"}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

/** LocalPreview binds the local camera track to a muted <video>. */
function LocalPreview({ track }: { track: MediaStreamTrack | null }) {
  const ref = useRef<HTMLVideoElement | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.srcObject = track ? new MediaStream([track]) : null;
  }, [track]);
  return <video className="call-preview" ref={ref} autoPlay muted playsInline />;
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
