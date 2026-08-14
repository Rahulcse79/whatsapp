// REST auth client: OTP request/verify, optional PIN (SIM-swap defense), and
// token refresh with rotation (auth-users-api.md §Auth). Wire JSON is
// snake_case; this maps it to camelCase and normalises errors to a code.

import type { HttpClient } from "./ports";

export type Platform = "ios" | "android" | "web";

export interface DeviceInfo {
  name: string;
  platform: Platform;
  /** Base64 public identity key. Optional: verifyOtp mints a per-device dev key
   *  when absent, so registration always carries the identity_key the server
   *  requires. */
  identityKey?: string;
}

export interface OtpChallenge {
  challengeId: string;
  channel: string;
}

export interface VerifiedSession {
  accessJwt: string;
  refreshToken: string;
  deviceId: string;
  requiresPin: boolean;
}

export interface TokenPair {
  accessJwt: string;
  refreshToken: string;
}

export class AuthApiError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
  ) {
    super(code);
    this.name = "AuthApiError";
  }
}

export class OtpClient {
  constructor(
    private readonly http: HttpClient,
    private readonly basePath = "/v1/auth",
  ) {}

  async requestOtp(phone: string): Promise<OtpChallenge> {
    const b = await this.call("/request-otp", { phone });
    return { challengeId: str(b.challenge_id), channel: str(b.channel) };
  }

  async verifyOtp(challengeId: string, code: string, device: DeviceInfo): Promise<VerifiedSession> {
    // The server reads `device` (not `device_info`) and REQUIRES a non-empty
    // base64 `identity_key` to register the device — without it registration
    // fails with VALIDATION_DEVICE ("couldn't sign in").
    const b = await this.call("/verify-otp", {
      challenge_id: challengeId,
      code,
      device: {
        name: device.name,
        platform: device.platform,
        identity_key: device.identityKey ?? newDeviceIdentityKey(),
      },
    });
    return session(b);
  }

  async verifyPin(challengeId: string, pin: string): Promise<VerifiedSession> {
    const b = await this.call("/verify-pin", { challenge_id: challengeId, pin });
    return session(b);
  }

  async refresh(refreshToken: string): Promise<TokenPair> {
    const b = await this.call("/refresh", { refresh_token: refreshToken });
    return { accessJwt: str(b.access_jwt), refreshToken: str(b.refresh_token) };
  }

  private async call(path: string, body: unknown): Promise<Record<string, unknown>> {
    const res = await this.http.post(`${this.basePath}${path}`, body);
    const parsed = asRecord(await res.json());
    if (res.status >= 400) throw new AuthApiError(codeOf(parsed), res.status);
    return parsed;
  }
}

function session(b: Record<string, unknown>): VerifiedSession {
  return {
    accessJwt: str(b.access_jwt),
    refreshToken: str(b.refresh_token),
    deviceId: str(b.device_id),
    requiresPin: Boolean(b.requires_pin ?? false),
  };
}

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function codeOf(b: Record<string, unknown>): string {
  return typeof b.code === "string" ? b.code : "UNKNOWN";
}

// newDeviceIdentityKey mints a 32-byte device identity key as standard base64
// (encoding/base64.StdEncoding on the Go side). The server requires a non-empty
// identity_key to register; real Signal identity-key management is a separate
// concern. Math.random keeps this RN-safe (no crypto.getRandomValues polyfill),
// and the base64 is hand-rolled so client-core needs no extra dependency.
const B64_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

function newDeviceIdentityKey(): string {
  const b = new Uint8Array(32);
  for (let i = 0; i < b.length; i++) b[i] = Math.floor(Math.random() * 256);
  let out = "";
  for (let i = 0; i < b.length; i += 3) {
    const b0 = b[i]!;
    const has1 = i + 1 < b.length;
    const has2 = i + 2 < b.length;
    const b1 = has1 ? b[i + 1]! : 0;
    const b2 = has2 ? b[i + 2]! : 0;
    out += B64_ALPHABET.charAt(b0 >> 2);
    out += B64_ALPHABET.charAt(((b0 & 0x03) << 4) | (b1 >> 4));
    out += has1 ? B64_ALPHABET.charAt(((b1 & 0x0f) << 2) | (b2 >> 6)) : "=";
    out += has2 ? B64_ALPHABET.charAt(b2 & 0x3f) : "=";
  }
  return out;
}
