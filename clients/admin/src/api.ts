// Thin client over the /admin/v1 REST surface. Auth is an OIDC ID token the
// operator obtains from the deployment's IdP (the SPA holds it; the redirect/
// PKCE flow is the IdP integration). The token + API base persist in
// sessionStorage so a reload doesn't lose them, but they never touch disk.
//
// JSON note: reports/users/audit structs carry no json tags server-side, so
// their keys are PascalCase (ID, TargetUserID, …); flags use snake_case.

const TOKEN_KEY = "wa.admin.token";
const BASE_KEY = "wa.admin.base";

export function getToken(): string {
  return sessionStorage.getItem(TOKEN_KEY) ?? "";
}
export function getBase(): string {
  return sessionStorage.getItem(BASE_KEY) ?? defaultBase();
}
export function saveSession(base: string, token: string): void {
  sessionStorage.setItem(BASE_KEY, base.replace(/\/+$/, ""));
  sessionStorage.setItem(TOKEN_KEY, token.trim());
}
export function clearSession(): void {
  sessionStorage.removeItem(TOKEN_KEY);
}

/** defaultBase guesses the core-api origin from the current host on :8080. */
function defaultBase(): string {
  try {
    return `${location.protocol}//${location.hostname}:8080`;
  } catch {
    return "http://localhost:8080";
  }
}

/** AdminError carries the HTTP status so the UI can send 401/403 back to login. */
export class AdminError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(getBase() + path, {
    method,
    headers: {
      authorization: `Bearer ${getToken()}`,
      ...(body === undefined ? {} : { "content-type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const data = text ? (JSON.parse(text) as unknown) : {};
  if (!res.ok) {
    const msg = (data as { error?: { message?: string } })?.error?.message ?? `HTTP ${res.status}`;
    throw new AdminError(res.status, msg);
  }
  return data as T;
}

// ── DTOs (mirror the Go structs) ────────────────────────────────────────────

export interface Report {
  ID: string;
  ReporterID: string;
  TargetUserID: string;
  Reason: number;
  Note: string;
  State: number;
  HasDisclosure: boolean;
  CreatedAt: string;
}
export interface UserSummary {
  ID: string;
  Username: string;
  DisplayName: string;
  Status: number; // 0 active | 1 suspended | 2 deleted
  DeviceCount: number;
  ReportCount: number;
  CreatedAt: string;
}
export interface AuditRecord {
  ID: number;
  Actor: string;
  Action: string;
  Target: string;
  Reason: string;
  At: string;
}
export interface FlagRule {
  enabled: boolean;
  rollout: number;
  allow?: string[];
  deny?: string[];
}
export interface NamedFlag {
  flag: string;
  rule: FlagRule;
  updated_by?: string;
  updated_at: string;
}

export type Resolution = "dismiss" | "warn" | "suspend";

// ── endpoints ───────────────────────────────────────────────────────────────

export const api = {
  listReports: () => req<{ reports: Report[] }>("GET", "/admin/v1/reports?limit=100").then((r) => r.reports ?? []),
  getReport: (id: string) => req<Report>("GET", `/admin/v1/reports/${id}`),
  resolveReport: (id: string, resolution: Resolution, reason: string) =>
    req<void>("POST", `/admin/v1/reports/${id}/resolve`, { resolution, reason }),

  searchUsers: (q: string) =>
    req<{ users: UserSummary[] }>("GET", `/admin/v1/users?q=${encodeURIComponent(q)}&limit=50`).then((r) => r.users ?? []),
  getUser: (id: string) => req<UserSummary>("GET", `/admin/v1/users/${id}`),
  suspendUser: (id: string, reason: string) => req<void>("POST", `/admin/v1/users/${id}/suspend`, { reason }),
  reactivateUser: (id: string, reason: string) => req<void>("POST", `/admin/v1/users/${id}/reactivate`, { reason }),

  listAudit: () => req<{ audit: AuditRecord[] }>("GET", "/admin/v1/audit?limit=200").then((r) => r.audit ?? []),

  listFlags: () => req<{ flags: NamedFlag[] }>("GET", "/admin/v1/flags").then((r) => r.flags ?? []),
  setFlag: (flag: string, rule: FlagRule, reason: string) =>
    req<void>("PUT", `/admin/v1/flags/${flag}`, { rule, reason }),
  deleteFlag: (flag: string, reason: string) =>
    req<void>("DELETE", `/admin/v1/flags/${flag}?reason=${encodeURIComponent(reason)}`),
};
