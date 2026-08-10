import { describe, expect, it } from "vitest";
import { AuthApiError, OtpClient } from "./otpClient";
import type { HttpClient, HttpResponse } from "./ports";

type Responder = (path: string, body: unknown) => { status: number; body: unknown };

class FakeHttp implements HttpClient {
  readonly calls: Array<{ path: string; body: unknown }> = [];
  constructor(private readonly responder: Responder) {}
  post(path: string, body: unknown): Promise<HttpResponse> {
    this.calls.push({ path, body });
    const r = this.responder(path, body);
    return Promise.resolve({ status: r.status, json: () => Promise.resolve(r.body) });
  }
}

describe("OtpClient", () => {
  it("requests an OTP and maps the challenge", async () => {
    const http = new FakeHttp(() => ({ status: 200, body: { challenge_id: "ch1", channel: "sms" } }));
    const otp = new OtpClient(http);
    const ch = await otp.requestOtp("+14155550123");
    expect(ch).toEqual({ challengeId: "ch1", channel: "sms" });
    expect(http.calls[0]?.path).toBe("/v1/auth/request-otp");
    expect(http.calls[0]?.body).toEqual({ phone: "+14155550123" });
  });

  it("verifies an OTP into a session (snake_case → camelCase)", async () => {
    const http = new FakeHttp(() => ({
      status: 200,
      body: { access_jwt: "a", refresh_token: "r", device_id: "d", requires_pin: false },
    }));
    const otp = new OtpClient(http);
    const s = await otp.verifyOtp("ch1", "123456", { name: "Pixel", platform: "android" });
    expect(s).toEqual({ accessJwt: "a", refreshToken: "r", deviceId: "d", requiresPin: false });
    expect(http.calls[0]?.body).toEqual({
      challenge_id: "ch1",
      code: "123456",
      device_info: { name: "Pixel", platform: "android" },
    });
  });

  it("surfaces the wire error code on 4xx", async () => {
    const http = new FakeHttp(() => ({ status: 401, body: { code: "AUTH_OTP_INVALID" } }));
    const otp = new OtpClient(http);
    await expect(otp.verifyOtp("ch1", "000000", { name: "x", platform: "ios" })).rejects.toMatchObject({
      code: "AUTH_OTP_INVALID",
      status: 401,
    });
    await expect(otp.verifyOtp("ch1", "000000", { name: "x", platform: "ios" })).rejects.toBeInstanceOf(AuthApiError);
  });

  it("rotates tokens on refresh", async () => {
    const http = new FakeHttp(() => ({ status: 200, body: { access_jwt: "a2", refresh_token: "r2" } }));
    const otp = new OtpClient(http);
    expect(await otp.refresh("r1")).toEqual({ accessJwt: "a2", refreshToken: "r2" });
  });
});
