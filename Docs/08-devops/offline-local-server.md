# Offline / Self-Hosted Local-Server Profile

| Doc | Operational guide for the fully self-hosted deployment |
|---|---|
| Status | v1.0 · Normative source: [/HLD.md](../../HLD.md) §17.5 — this doc adds operational detail |
| Rule | Offline capability is a **values overlay**, never a fork. Same binaries, charts, protobuf. |

## 1. Profiles

| | Profile A — local server, internet available | Profile B — air-gapped LAN |
|---|---|---|
| Push | keep FCM/APNs **or** self-host ntfy | ntfy (UnifiedPush) mandatory; iOS degraded (see §4) |
| OTP | keep SMS or email | email (local SMTP) / TOTP / admin-provisioned |
| Certs | Let's Encrypt | step-ca private CA |
| CI/registry | GitHub or Gitea | Gitea + Harbor, mirrored images |
| GIF proxy | optional | disabled; local sticker packs |

## 2. Substitution stack (operational notes)

| Concern | Component | Notes |
|---|---|---|
| Push | **ntfy** (self-hosted) | notification-svc ntfy driver; Android clients hold one lightweight conn to ntfy; battery cost ≈ FCM within margin when self-hosted on LAN |
| Web push | VAPID WebPush | works fully offline against local endpoint |
| OTP | Stalwart/Postfix SMTP → email codes; TOTP enrollment for 2FA | identity anchor = username/email; GSM modem (gammu-smsd) optional if phone identity kept |
| TLS | **step-ca**: offline root (HSM/USB, powered off), 1-y intermediates, ACME for cert-manager | CA root pre-installed on enrolled devices; mobile pinning pins the private CA |
| DNS | CoreDNS/dnsmasq authoritative local zone | split-horizon if Profile A |
| LB | MetalLB (K3s/K8s) or HAProxy+keepalived | |
| Time | chrony server (GPS/RTC source if truly air-gapped) | **not optional**: JWT, TOTP, TLS all break on drift |
| Git/CI | Gitea + Gitea Actions/Woodpecker; ArgoCD → local Gitea | GitOps unchanged |
| Registry | Harbor (or registry:2) | air-gap mirror procedure below |
| Crash/analytics | GlitchTip/Sentry self-hosted | already offline-capable |
| App distribution | PWA from local web server; Android APK/local F-Droid repo | iOS needs Apple infra — Profile B iOS is best-effort |

## 3. Air-gap image mirroring (runbook seed)

On a connected staging machine: `crane pull` the pinned image list (generated from chart values) → `crane push` to portable OCI layout → transfer (disk) → `crane push` into Harbor. Signatures travel with images (cosign attach); admission policy unchanged inside the air gap. Same flow for Go module / npm proxy caches (Athens / Verdaccio) if building inside the gap.

## 4. Honest limits (set expectations with stakeholders)

1. **iOS in Profile B:** no APNs ⇒ no locked-phone wake/ring. Foreground/background-fetch delivery only. **Android + PWA are the first-class offline clients.**
2. **Phone-number identity** needs a phone network; Profile B identity = username/email + TOTP.
3. **Single box = no HA.** SLO becomes best-effort; nightly backups to a second disk/box; two-box upgrade path in capacity-planning.md §4.

## 5. Single-box layouts

| Tier | Runs |
|---|---|
| Pilot | `deploy/compose/`: full stack, one `docker compose up` — PG, Valkey, NATS, MinIO (single-node multi-drive), LiveKit, coturn, ntfy, step-ca, Grafana bundle, all 4 Go deployables |
| Production | K3s single node, full Helm stack minus multi-node HA (values: replicas 1, Sentinel off, MinIO SNMD); second box moves PG replica + backup target + MinIO mirror off-box |

## 6. Operator runbook index (to be written in ops/runbooks/ during P0)

Bootstrap from bare metal → cert rotation (intermediate + device re-trust) → backup verify → restore drill → image mirror refresh → user provisioning (Profile B) → capacity watch (single-box SLIs).
