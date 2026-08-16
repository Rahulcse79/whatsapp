// On-device AI runtime (T11.01). A framework-free abstraction that gates every
// AI task behind: (1) the operator kill-switch, (2) a per-mode user consent, and
// (3) an available provider. Default is OFF. On-device mode never sends content
// anywhere; the opt-in "server" mode requires an explicit disclosure consent
// because the specific content run through AI leaves the device (and is no longer
// E2EE for that request). The actual models/endpoints are injected providers —
// T11.02/T11.03 supply them.

export type AiMode = "off" | "on-device" | "server";

export interface AiConsent {
  /** consented to on-device AI (nothing leaves the device) */
  onDevice: boolean;
  /** consented to the opt-in server endpoint (content leaves the device — disclosed) */
  server: boolean;
}

export interface AiSettings {
  mode: AiMode;
  consent: AiConsent;
}

export const DEFAULT_AI_SETTINGS: AiSettings = { mode: "off", consent: { onDevice: false, server: false } };

/** AiTask is a generic request; T11.02/T11.03 specialise `kind` (smart-reply,
 *  summarize, translate, transcribe, …). */
export interface AiTask {
  kind: string;
  input: string;
}

export interface AiResult {
  text: string;
}

/** AiProvider runs a task — an on-device model or a disclosed server endpoint. */
export interface AiProvider {
  run(task: AiTask): Promise<AiResult>;
}

export type AiUnavailableReason = "kill-switch" | "disabled" | "consent-required" | "no-provider" | "endpoint-unavailable";

export class AiUnavailable extends Error {
  constructor(public readonly reason: AiUnavailableReason) {
    super(`AI unavailable: ${reason}`);
    this.name = "AiUnavailable";
  }
}

export interface AiRuntimeDeps {
  /** operator kill-switch: false = AI disabled org-wide (GET /v1/ai/config) */
  killSwitchOn: () => boolean;
  /** whether an opt-in server-inference endpoint is provisioned */
  serverEndpointAvailable: () => boolean;
  /** current user AI settings (mode + consent) */
  settings: () => AiSettings;
  onDevice?: AiProvider;
  server?: AiProvider;
}

export class AiRuntime {
  constructor(private readonly deps: AiRuntimeDeps) {}

  /** availability returns null when a task can run now, else why it can't. */
  availability(): AiUnavailableReason | null {
    if (!this.deps.killSwitchOn()) return "kill-switch";
    const s = this.deps.settings();
    if (s.mode === "off") return "disabled";
    if (s.mode === "on-device") {
      if (!s.consent.onDevice) return "consent-required";
      if (!this.deps.onDevice) return "no-provider";
      return null;
    }
    // server (opt-in, disclosed)
    if (!this.deps.serverEndpointAvailable()) return "endpoint-unavailable";
    if (!s.consent.server) return "consent-required";
    if (!this.deps.server) return "no-provider";
    return null;
  }

  available(): boolean {
    return this.availability() === null;
  }

  /** run executes a task, throwing AiUnavailable if any gate fails. */
  async run(task: AiTask): Promise<AiResult> {
    const reason = this.availability();
    if (reason) throw new AiUnavailable(reason);
    const s = this.deps.settings();
    const provider = s.mode === "server" ? this.deps.server : this.deps.onDevice;
    if (!provider) throw new AiUnavailable("no-provider");
    return provider.run(task);
  }
}

/** disclosureFor is the consent copy shown before enabling a mode. */
export function disclosureFor(mode: AiMode): string {
  switch (mode) {
    case "on-device":
      return "AI features run entirely on your device. Your messages are never sent anywhere for AI processing, and stay end-to-end encrypted.";
    case "server":
      return "You're opting in to server-side AI. Only the specific content you run through an AI feature is sent to the AI service for that request — it is not end-to-end encrypted for that request. Nothing else leaves your device.";
    default:
      return "AI features are off. No AI runs on your messages.";
  }
}

/** requiresDisclosure is true for modes that send content off the device — the
 *  UI must show and acknowledge disclosureFor() before enabling them. */
export function requiresDisclosure(mode: AiMode): boolean {
  return mode === "server";
}
