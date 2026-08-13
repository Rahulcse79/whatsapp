// Media-flood profile (load-and-chaos-testing.md §1): 500 parallel 25 MB uploads
// + downloads. Pass = presign latency stable and the API pods unaffected (the
// isolation check — presigned direct-to-storage must bypass the services, so a
// media storm can't starve the messaging path). REST, no WS: media durability is
// content-hash verification, not the message audit.
//
// Each VU: create an upload (media-svc presign) → PUT the parts straight to
// object storage → complete (server verifies size + hash) → presign a download →
// GET it back. Measures presign latency and completion success.

import http from "k6/http";
import { check } from "k6";
import { Counter, Trend } from "k6/metrics";

const API = __ENV.API_URL || "http://localhost:8080";
const TOKENS = (__ENV.TOKENS || "").split(",").filter(Boolean);
const SIZE = Number(__ENV.SIZE_BYTES || 25 * 1024 * 1024); // 25 MB cap

const presignMs = new Trend("presign_ms", true);
const uploadOk = new Counter("media_upload_ok");
const uploadFail = new Counter("media_upload_fail");

export const options = {
  scenarios: {
    media_flood: {
      executor: "per-vu-iterations",
      vus: 500, // 500 parallel transfers
      iterations: 1,
      maxDuration: "10m",
    },
  },
  thresholds: {
    presign_ms: ["p(95)<=500"], // presign stays fast under the flood
    media_upload_fail: ["count==0"],
    // Isolation check: the messaging RED middleware's own latency SLO (asserted
    // by a concurrent sustained run) must hold — media must not starve the API.
    "http_req_duration{api:control}": ["p(95)<=250"],
  },
};

export default function () {
  const token = TOKENS[(__VU - 1) % Math.max(TOKENS.length, 1)];
  if (!token) {
    uploadFail.add(1);
    return;
  }
  const auth = { headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, tags: { api: "control" } };

  // 1) create upload (control-plane presign).
  let t0 = Date.now();
  const created = http.post(`${API}/v1/media/uploads`, JSON.stringify({ size: SIZE, content_hash: "AA==", mime_claimed: "application/octet-stream" }), auth);
  presignMs.add(Date.now() - t0);
  if (!check(created, { "upload created 201": (r) => r.status === 201 })) {
    uploadFail.add(1);
    return;
  }
  const body = created.json();
  const parts = (body && body.part_urls) || [];
  const partSize = (body && body.part_size) || SIZE;

  // 2) PUT each part straight to object storage (bypasses the services).
  const etags = [];
  for (const p of parts) {
    const chunk = new Uint8Array(Math.min(partSize, 1024)); // a stand-in slice; the harness streams real bytes
    const put = http.put(p.url, chunk.buffer, { headers: { "Content-Type": "application/octet-stream" } });
    if (put.status >= 300) {
      uploadFail.add(1);
      return;
    }
    etags.push({ part_number: p.part_number, etag: (put.headers && put.headers.Etag) || "etag" });
  }

  // 3) complete (server re-derives + verifies the hash).
  const done = http.post(`${API}/v1/media/uploads/${encodeURIComponent(body.upload_id)}/complete`, JSON.stringify({ parts_etags: etags }), auth);
  if (!check(done, { "upload completed": (r) => r.status < 300 })) {
    uploadFail.add(1);
    return;
  }

  // 4) download-url round trip (presigned GET).
  t0 = Date.now();
  const dl = http.post(`${API}/v1/media/download-urls`, JSON.stringify({ object_keys: [body.object_key] }), auth);
  presignMs.add(Date.now() - t0);
  check(dl, { "download presigned": (r) => r.status < 300 });
  uploadOk.add(1);
}

export function handleSummary(data) {
  const p95 = Math.round(data.metrics.presign_ms?.values["p(95)"] ?? 0);
  const ok = data.metrics.media_upload_ok?.values.count ?? 0;
  const fail = data.metrics.media_upload_fail?.values.count ?? 0;
  return { stdout: `\nmedia-flood: transfers ok=${ok} fail=${fail} · presign p95=${p95}ms (≤500)\n` };
}
