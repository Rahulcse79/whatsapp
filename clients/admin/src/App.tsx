import { useCallback, useEffect, useState } from "react";
import {
  AdminError,
  api,
  clearSession,
  getBase,
  getToken,
  saveSession,
  type AuditRecord,
  type NamedFlag,
  type Report,
  type Resolution,
  type UserSummary,
} from "./api";

type Tab = "reports" | "users" | "flags" | "audit";
const USER_STATUS = ["active", "suspended", "deleted"];

export function App() {
  const [authed, setAuthed] = useState(() => getToken() !== "");
  const [tab, setTab] = useState<Tab>("reports");
  const signOut = useCallback(() => {
    clearSession();
    setAuthed(false);
  }, []);

  if (!authed) return <Login onSignedIn={() => setAuthed(true)} />;

  return (
    <div className="app">
      <header>
        <h1>WhatsApp V2 · Admin</h1>
        <nav>
          {(["reports", "users", "flags", "audit"] as Tab[]).map((t) => (
            <button key={t} className={tab === t ? "tab active" : "tab"} onClick={() => setTab(t)}>
              {t.charAt(0).toUpperCase() + t.slice(1)}
            </button>
          ))}
          <button className="tab signout" onClick={signOut}>
            Sign out
          </button>
        </nav>
        <p className="base">{getBase()}</p>
      </header>
      <main>
        {tab === "reports" && <Reports onAuthError={signOut} />}
        {tab === "users" && <Users onAuthError={signOut} />}
        {tab === "flags" && <Flags onAuthError={signOut} />}
        {tab === "audit" && <Audit onAuthError={signOut} />}
      </main>
    </div>
  );
}

function Login({ onSignedIn }: { onSignedIn: () => void }) {
  const [base, setBase] = useState(getBase());
  const [token, setToken] = useState("");
  return (
    <div className="login">
      <h1>Admin console</h1>
      <p className="muted">
        Sign in with the OIDC ID token your identity provider issues for the admin role. The token is held in
        this tab only (sessionStorage) and sent as a bearer credential to <code>/admin/v1</code>.
      </p>
      <label>
        API base
        <input value={base} onChange={(e) => setBase(e.target.value)} placeholder="https://api.example.com" />
      </label>
      <label>
        OIDC ID token
        <textarea value={token} onChange={(e) => setToken(e.target.value)} rows={4} placeholder="eyJ…" />
      </label>
      <button
        className="primary"
        disabled={!token.trim()}
        onClick={() => {
          saveSession(base, token);
          onSignedIn();
        }}
      >
        Sign in
      </button>
    </div>
  );
}

/** useLoader runs an async fetch, routing 401/403 back to sign-in. */
function useLoader<T>(
  load: () => Promise<T>,
  onAuthError: () => void,
  deps: unknown[],
): { data: T | null; error: string | null; loading: boolean; reload: () => void } {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [nonce, setNonce] = useState(0);
  useEffect(() => {
    let alive = true;
    setLoading(true);
    setError(null);
    load()
      .then((d) => alive && setData(d))
      .catch((e: unknown) => {
        if (!alive) return;
        if (e instanceof AdminError && (e.status === 401 || e.status === 403)) onAuthError();
        setError(e instanceof Error ? e.message : "Request failed");
      })
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nonce, ...deps]);
  return { data, error, loading, reload: () => setNonce((n) => n + 1) };
}

async function action(fn: () => Promise<void>, onAuthError: () => void, onDone: () => void, onErr: (m: string) => void): Promise<void> {
  try {
    await fn();
    onDone();
  } catch (e) {
    if (e instanceof AdminError && (e.status === 401 || e.status === 403)) onAuthError();
    onErr(e instanceof Error ? e.message : "Action failed");
  }
}

function Reports({ onAuthError }: { onAuthError: () => void }) {
  const { data, error, loading, reload } = useLoader(() => api.listReports(), onAuthError, []);
  const [sel, setSel] = useState<Report | null>(null);
  const [resolution, setResolution] = useState<Resolution>("dismiss");
  const [reason, setReason] = useState("");
  const [msg, setMsg] = useState<string | null>(null);

  return (
    <section>
      <div className="head">
        <h2>Open reports {data ? `(${data.length})` : ""}</h2>
        <button onClick={reload}>Refresh</button>
      </div>
      {loading && <p className="muted">Loading…</p>}
      {error && <p className="error">{error}</p>}
      {data && data.length === 0 && <p className="muted">The queue is empty.</p>}
      <table>
        <tbody>
          {(data ?? []).map((r) => (
            <tr key={r.ID} className={sel?.ID === r.ID ? "row selected" : "row"} onClick={() => { setSel(r); setMsg(null); }}>
              <td className="mono">{r.TargetUserID.slice(0, 8)}</td>
              <td>reason #{r.Reason}</td>
              <td className="muted">{r.Note || "—"}</td>
              <td className="muted">{new Date(r.CreatedAt).toLocaleString()}</td>
            </tr>
          ))}
        </tbody>
      </table>

      {sel && (
        <div className="detail">
          <h3>Resolve report</h3>
          <p className="mono">target {sel.TargetUserID}</p>
          <p className="muted">reporter {sel.ReporterID || "(deleted)"} · reason #{sel.Reason}{sel.HasDisclosure ? " · has disclosure" : ""}</p>
          <label>
            Resolution
            <select value={resolution} onChange={(e) => setResolution(e.target.value as Resolution)}>
              <option value="dismiss">Dismiss (no wrongdoing)</option>
              <option value="warn">Warn the target</option>
              <option value="suspend">Suspend the target</option>
            </select>
          </label>
          <label>
            Reason (audit)
            <input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Why?" />
          </label>
          {msg && <p className="muted">{msg}</p>}
          <button
            className="primary"
            onClick={() =>
              void action(
                () => api.resolveReport(sel.ID, resolution, reason),
                onAuthError,
                () => { setSel(null); setReason(""); reload(); },
                (m) => setMsg(m),
              )
            }
          >
            Apply
          </button>
        </div>
      )}
    </section>
  );
}

function Users({ onAuthError }: { onAuthError: () => void }) {
  const [q, setQ] = useState("");
  const [query, setQuery] = useState("");
  const { data, error, loading, reload } = useLoader(() => (query.trim() ? api.searchUsers(query) : Promise.resolve<UserSummary[]>([])), onAuthError, [query]);
  const [reason, setReason] = useState("");
  const [msg, setMsg] = useState<string | null>(null);

  return (
    <section>
      <div className="head">
        <h2>User search</h2>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(q);
        }}
        className="searchbar"
      >
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="username or phone hash…" />
        <button className="primary" type="submit">
          Search
        </button>
      </form>
      {loading && <p className="muted">Searching…</p>}
      {error && <p className="error">{error}</p>}
      {msg && <p className="muted">{msg}</p>}
      {data && query.trim() && data.length === 0 && <p className="muted">No matching users.</p>}
      <table>
        <tbody>
          {(data ?? []).map((u) => (
            <tr key={u.ID} className="row">
              <td>
                <strong>{u.DisplayName || "—"}</strong> <span className="muted">@{u.Username}</span>
                <br />
                <span className="mono muted">{u.ID}</span>
              </td>
              <td>
                <span className={`badge s${u.Status}`}>{USER_STATUS[u.Status] ?? u.Status}</span>
                <br />
                <span className="muted">{u.DeviceCount} devices · {u.ReportCount} reports</span>
              </td>
              <td>
                <input placeholder="reason" onChange={(e) => setReason(e.target.value)} className="small" />
                {u.Status === 1 ? (
                  <button onClick={() => void action(() => api.reactivateUser(u.ID, reason), onAuthError, () => { setMsg("Reactivated."); reload(); }, setMsg)}>
                    Reactivate
                  </button>
                ) : (
                  <button className="danger" onClick={() => void action(() => api.suspendUser(u.ID, reason), onAuthError, () => { setMsg("Suspended."); reload(); }, setMsg)}>
                    Suspend
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

function Flags({ onAuthError }: { onAuthError: () => void }) {
  const { data, error, loading, reload } = useLoader(() => api.listFlags(), onAuthError, []);
  const [flag, setFlag] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [rollout, setRollout] = useState(100);
  const [reason, setReason] = useState("");
  const [msg, setMsg] = useState<string | null>(null);

  return (
    <section>
      <div className="head">
        <h2>Feature flags {data ? `(${data.length})` : ""}</h2>
        <button onClick={reload}>Refresh</button>
      </div>
      {loading && <p className="muted">Loading…</p>}
      {error && <p className="error">{error}</p>}
      {msg && <p className="muted">{msg}</p>}
      <table>
        <tbody>
          {(data ?? []).map((f: NamedFlag) => (
            <tr key={f.flag} className="row">
              <td className="mono">{f.flag}</td>
              <td>
                {f.rule.enabled ? "on" : "off"} · {f.rule.rollout}%
                {f.rule.allow?.length ? ` · +${f.rule.allow.length} allow` : ""}
                {f.rule.deny?.length ? ` · −${f.rule.deny.length} deny` : ""}
              </td>
              <td className="muted">{f.updated_by || "—"}</td>
              <td>
                <button
                  className="danger"
                  onClick={() => {
                    const why = window.prompt(`Delete flag "${f.flag}"? Reason:`);
                    if (why) void action(() => api.deleteFlag(f.flag, why), onAuthError, () => { setMsg(`Deleted ${f.flag}.`); reload(); }, setMsg);
                  }}
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <div className="detail">
        <h3>Create / update a flag</h3>
        <div className="row-inputs">
          <label>
            Flag
            <input value={flag} onChange={(e) => setFlag(e.target.value)} placeholder="media_uploads" />
          </label>
          <label className="check">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} /> Enabled
          </label>
          <label>
            Rollout %
            <input type="number" min={0} max={100} value={rollout} onChange={(e) => setRollout(Number(e.target.value))} />
          </label>
        </div>
        <label>
          Reason (audit)
          <input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Why?" />
        </label>
        <button
          className="primary"
          disabled={!flag.trim()}
          onClick={() =>
            void action(
              () => api.setFlag(flag.trim(), { enabled, rollout: Math.max(0, Math.min(100, rollout)) }, reason),
              onAuthError,
              () => { setMsg(`Saved ${flag}.`); setFlag(""); reload(); },
              setMsg,
            )
          }
        >
          Save flag
        </button>
      </div>
    </section>
  );
}

function Audit({ onAuthError }: { onAuthError: () => void }) {
  const { data, error, loading, reload } = useLoader(() => api.listAudit(), onAuthError, []);
  return (
    <section>
      <div className="head">
        <h2>Audit log {data ? `(${data.length})` : ""}</h2>
        <button onClick={reload}>Refresh</button>
      </div>
      {loading && <p className="muted">Loading…</p>}
      {error && <p className="error">{error} {" "}<span className="muted">(owner-only)</span></p>}
      <table>
        <tbody>
          {(data ?? []).map((a: AuditRecord) => (
            <tr key={a.ID} className="row">
              <td className="mono">{a.Action}</td>
              <td className="mono muted">{a.Target ? a.Target.slice(0, 8) : "—"}</td>
              <td>{a.Reason || "—"}</td>
              <td className="muted">{a.Actor}</td>
              <td className="muted">{new Date(a.At).toLocaleString()}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
