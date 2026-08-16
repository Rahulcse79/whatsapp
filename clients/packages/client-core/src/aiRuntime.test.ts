import { describe, expect, it } from "vitest";
import {
  AiRuntime,
  AiUnavailable,
  disclosureFor,
  requiresDisclosure,
  type AiProvider,
  type AiSettings,
} from "./aiRuntime";

const echo: AiProvider = { run: (t) => Promise.resolve({ text: `echo:${t.input}` }) };

function runtime(over: Partial<{ kill: boolean; endpoint: boolean; settings: AiSettings; onDevice?: AiProvider; server?: AiProvider }> = {}) {
  return new AiRuntime({
    killSwitchOn: () => over.kill ?? true,
    serverEndpointAvailable: () => over.endpoint ?? false,
    settings: () => over.settings ?? { mode: "off", consent: { onDevice: false, server: false } },
    onDevice: "onDevice" in over ? over.onDevice : echo,
    server: "server" in over ? over.server : echo,
  });
}

describe("AiRuntime gating", () => {
  it("is off by default", () => {
    expect(runtime().availability()).toBe("disabled");
  });

  it("respects the operator kill-switch above everything", () => {
    const r = runtime({ kill: false, settings: { mode: "on-device", consent: { onDevice: true, server: true } } });
    expect(r.availability()).toBe("kill-switch");
  });

  it("on-device requires consent, then runs without leaving the device", async () => {
    expect(runtime({ settings: { mode: "on-device", consent: { onDevice: false, server: false } } }).availability()).toBe("consent-required");
    const r = runtime({ settings: { mode: "on-device", consent: { onDevice: true, server: false } } });
    expect(r.available()).toBe(true);
    expect((await r.run({ kind: "summarize", input: "hi" })).text).toBe("echo:hi");
  });

  it("server mode needs an endpoint AND disclosure consent", async () => {
    // endpoint not provisioned
    expect(runtime({ settings: { mode: "server", consent: { onDevice: false, server: true } } }).availability()).toBe("endpoint-unavailable");
    // endpoint up but no consent
    expect(runtime({ endpoint: true, settings: { mode: "server", consent: { onDevice: false, server: false } } }).availability()).toBe("consent-required");
    // endpoint up + consent → runs via the server provider
    const r = runtime({ endpoint: true, settings: { mode: "server", consent: { onDevice: false, server: true } }, server: { run: () => Promise.resolve({ text: "server" }) } });
    expect((await r.run({ kind: "x", input: "y" })).text).toBe("server");
  });

  it("run() throws AiUnavailable when a gate fails", async () => {
    await expect(runtime().run({ kind: "x", input: "y" })).rejects.toBeInstanceOf(AiUnavailable);
  });

  it("reports no-provider when the mode's provider is missing", () => {
    const r = runtime({ settings: { mode: "on-device", consent: { onDevice: true, server: false } }, onDevice: undefined });
    expect(r.availability()).toBe("no-provider");
  });
});

describe("disclosure", () => {
  it("only server mode requires a disclosure acknowledgement", () => {
    expect(requiresDisclosure("server")).toBe(true);
    expect(requiresDisclosure("on-device")).toBe(false);
    expect(disclosureFor("server")).toMatch(/not end-to-end encrypted/i);
    expect(disclosureFor("on-device")).toMatch(/never sent anywhere/i);
  });
});
