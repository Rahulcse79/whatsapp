// REST auth client: OTP request/verify, optional PIN (SIM-swap defense), and
// token refresh with rotation (auth-users-api.md §Auth). Wire JSON is
// snake_case; this maps it to camelCase and normalises errors to a code.

import type { HttpClient } from "./ports";

export type Platform = "ios" | "android" | "web";

export interface DeviceInfo {
  name: string;
  platform: Platform;
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
    const b = await this.call("/verify-otp", {
      challenge_id: challengeId,
      code,
      device_info: { name: device.name, platform: device.platform },
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
