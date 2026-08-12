// Opus audio quality config (rtc-lld §3, HLD §10.5): DTX (discontinuous
// transmission — silence sends nothing) + in-band FEC (forward error correction,
// so voice survives ~30% loss), adaptive 6–32 kbps. Pure: it builds the codec
// parameters and munges them into an SDP — the portable way to enable DTX/FEC on
// WebRTC across web and react-native-webrtc. No RTC types.

export const OPUS_MIN_KBPS = 6;
export const OPUS_MAX_KBPS = 32;

export interface OpusParams {
  /** Discontinuous transmission — transmit nothing during silence. */
  dtx: boolean;
  /** In-band forward error correction — recovers lost packets. */
  fec: boolean;
  /** Target average bitrate in bits/sec. */
  maxAverageBitrateBps: number;
}

/** buildOpusParams returns DTX+FEC-on params at a bitrate clamped to the voice
 *  band (6–32 kbps). */
export function buildOpusParams(targetKbps = OPUS_MAX_KBPS): OpusParams {
  const kbps = Math.min(OPUS_MAX_KBPS, Math.max(OPUS_MIN_KBPS, Math.round(targetKbps)));
  return { dtx: true, fec: true, maxAverageBitrateBps: kbps * 1000 };
}

/** opusFmtpParams renders the fmtp `key=value` list for an Opus rtpmap. */
export function opusFmtpParams(p: OpusParams): string {
  return [`useinbandfec=${p.fec ? 1 : 0}`, `usedtx=${p.dtx ? 1 : 0}`, `maxaveragebitrate=${p.maxAverageBitrateBps}`].join(
    ";",
  );
}

/**
 * applyOpusConfig munges an SDP so the Opus payload advertises DTX+FEC and the
 * target bitrate: it finds the opus rtpmap payload type, then rewrites the
 * matching `a=fmtp:<pt>` line (preserving any other params) or inserts one. The
 * SDP is returned unchanged when Opus isn't present.
 */
export function applyOpusConfig(sdp: string, p: OpusParams): string {
  const pt = opusPayloadType(sdp);
  if (pt === null) return sdp;

  const lines = sdp.split(/\r?\n/);
  const fmtpIdx = lines.findIndex((l) => l.startsWith(`a=fmtp:${pt} `));
  if (fmtpIdx >= 0) {
    const cur = lines[fmtpIdx];
    if (cur !== undefined) lines[fmtpIdx] = mergeFmtp(cur, p);
  } else {
    const rtpmapIdx = lines.findIndex((l) => new RegExp(`^a=rtpmap:${pt} opus/`, "i").test(l));
    if (rtpmapIdx < 0) return sdp;
    lines.splice(rtpmapIdx + 1, 0, `a=fmtp:${pt} ${opusFmtpParams(p)}`);
  }
  return lines.join("\r\n");
}

function opusPayloadType(sdp: string): string | null {
  const m = sdp.match(/^a=rtpmap:(\d+) opus\/48000/im);
  return m && m[1] !== undefined ? m[1] : null;
}

/** mergeFmtp rewrites the DTX/FEC/bitrate keys in an existing fmtp line while
 *  keeping every other parameter (e.g. minptime). */
function mergeFmtp(line: string, p: OpusParams): string {
  const sp = line.indexOf(" ");
  const head = line.slice(0, sp + 1); // "a=fmtp:<pt> "
  const kv = new Map<string, string>();
  for (const e of line.slice(sp + 1).split(";")) {
    if (!e) continue;
    const [k, v] = e.split("=");
    if (k) kv.set(k.trim(), (v ?? "").trim());
  }
  kv.set("useinbandfec", p.fec ? "1" : "0");
  kv.set("usedtx", p.dtx ? "1" : "0");
  kv.set("maxaveragebitrate", String(p.maxAverageBitrateBps));
  return head + [...kv.entries()].map(([k, v]) => `${k}=${v}`).join(";");
}
