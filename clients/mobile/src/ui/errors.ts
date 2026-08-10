import { AuthApiError } from "../core/otpClient";

/** Turns an unknown thrown value into a short user-facing message. */
export function messageOf(e: unknown): string {
  if (e instanceof AuthApiError) return humanizeAuth(e.code);
  if (e instanceof Error && e.message) return e.message;
  return "Something went wrong. Please try again.";
}

function humanizeAuth(code: string): string {
  switch (code) {
    case "AUTH_OTP_INVALID":
      return "That code isn't right. Check it and try again.";
    case "AUTH_OTP_EXPIRED":
      return "That code expired. Request a new one.";
    case "AUTH_PIN_INVALID":
      return "Incorrect PIN.";
    case "RATE_LIMITED":
      return "Too many attempts. Wait a moment and retry.";
    case "VALIDATION_PHONE":
      return "That phone number doesn't look valid.";
    default:
      return "Couldn't sign in. Please try again.";
  }
}
