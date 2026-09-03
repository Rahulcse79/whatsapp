#!/usr/bin/env bash
#
# start.sh — bring up the whole WhatsApp V2 stack on one box for local dev.
#
#   ./start.sh            # up: infra → migrations → 4 Go services → ALL UIs
#   ./start.sh up [native|docker]
#   ./start.sh ui         # (re)start just the UIs against an already-up backend
#   ./start.sh down       # stop the Go services + UIs + infra
#   ./start.sh restart
#   ./start.sh status
#   ./start.sh logs [svc] # core-api|ws-gateway|media-svc|notification-svc
#                         # |web|admin|mobile|nats|minio|livekit|pnpm-install
#
# "Everything" means everything: infra, migrations, the four Go services, and
# all three front-ends — the web PWA (:5173), the admin console (:5174) and the
# Expo/Metro dev server for the mobile app (:8090). Opt out per UI with
# WA_SKIP_WEB=1 / WA_SKIP_ADMIN=1 / WA_SKIP_MOBILE=1, and suppress the browser
# tab with WA_NO_OPEN=1.
#
# Runtime is auto-detected: if Docker is running it uses deploy/compose; if not,
# it falls back to NATIVE services via Homebrew (no Docker needed). Force either
# with `./start.sh up native` / `./start.sh up docker` or WA_RUNTIME=native.
#
# Prereqs: Go (1.25 toolchain auto-fetched if yours is older).
#   • docker mode: Docker Desktop / a running daemon.
#   • native mode (macOS): Homebrew — it installs postgresql@17, nats-server,
#     minio, golang-migrate as needed, and reuses valkey or redis if present.
set -euo pipefail

# ── paths ──────────────────────────────────────────────────────────────────
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$REPO_ROOT/deploy/compose/docker-compose.yml"
COMPOSE_ENV="$REPO_ROOT/deploy/compose/.env"
RUN_DIR="$REPO_ROOT/.run"
LOG_DIR="$RUN_DIR/logs"
PID_DIR="$RUN_DIR/pids"
BIN_DIR="$RUN_DIR/bin"
MINIO_DATA="$RUN_DIR/minio-data"
SEED_FILE="$RUN_DIR/jwt-seed"

SERVICES=(core-api ws-gateway media-svc notification-svc)
UI_APPS=(web admin mobile)
# UI ports. Metro is pinned to 8090 on purpose: Expo defaults to 8081, which is
# ws-gateway here, and its usual fallback 8082 is media-svc — left alone, the
# mobile dev server either fails to bind or silently squats a service port.
WEB_PORT=5173
ADMIN_PORT=5174
METRO_PORT=8090
COMPOSE=(docker compose -f "$COMPOSE_FILE")
RUNTIME=""
MINIO_OK=1   # media-svc is started only when MinIO is available
# Prebuilt-download arch. buf names its release assets differently from
# nats/minio (x86_64 rather than amd64), hence the second variable.
case "$(uname -m)" in
  arm64) OS_ARCH=arm64; BUF_ARCH=arm64 ;;
  *)     OS_ARCH=amd64; BUF_ARCH=x86_64 ;;
esac
BUF_VER=v1.47.2
PROTO_STAMP="$RUN_DIR/proto.stamp"

# ── pretty output ───────────────────────────────────────────────────────────
if [ -t 1 ]; then B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; D=$'\033[2m'; X=$'\033[0m'; else B= G= Y= R= D= X=; fi
say()  { printf '%s▸ %s%s\n' "$B" "$*" "$X"; }
ok()   { printf '  %s✓%s %s\n' "$G" "$X" "$*"; }
warn() { printf '  %s!%s %s\n' "$Y" "$X" "$*"; }
die()  { printf '%s✗ %s%s\n' "$R" "$*" "$X" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing prerequisite: $1"; }
port_up()   { lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; }
port_wait() { local i; for i in $(seq 1 "${2:-30}"); do port_up "$1" && return 0; printf '.'; sleep 1; done; return 1; }
# http_wait URL [tries] — poll until the URL actually answers. A listening port
# is not the same as a served app: Vite binds before the first build finishes,
# and a crashed dev server can leave a stale socket. Everything that claims
# "started" below goes through here first.
http_wait() {
  local i
  for i in $(seq 1 "${2:-40}"); do
    curl -fsS -o /dev/null --max-time 2 "$1" >/dev/null 2>&1 && return 0
    printf '.'; sleep 1
  done
  return 1
}
# kill_tree PID — kill a process and its descendants. Needed because the UI dev
# servers fork children (Vite's optimizer, Metro's workers) that outlive a plain
# kill of the parent and keep holding the port.
kill_tree() {
  local pid="$1" child
  for child in $(pgrep -P "$pid" 2>/dev/null); do kill_tree "$child"; done
  kill "$pid" 2>/dev/null || true
}
# fetch_bin URL DEST — download a single prebuilt binary (official release host).
fetch_bin() { curl -fsSL "$1" -o "$2" && chmod +x "$2"; }
# lan_ip echoes this machine's WiFi/LAN address (for phones on the same network),
# falling back to localhost. Services bind 0.0.0.0 regardless — this is just the
# address to hand the app.
lan_ip() { ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo localhost; }

# ── runtime selection ───────────────────────────────────────────────────────
detect_runtime() {
  if [ -n "$RUNTIME" ]; then return; fi
  if [ -n "${WA_RUNTIME:-}" ]; then RUNTIME="$WA_RUNTIME"; return; fi
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then RUNTIME=docker; else RUNTIME=native; fi
}

# ── shared environment for every Go service ─────────────────────────────────
# One JWT seed is generated once and reused, so tokens minted by core-api verify
# in ws-gateway (and survive restarts). Dev config defaults cover PG/Valkey/NATS.
load_env() {
  mkdir -p "$RUN_DIR" "$LOG_DIR" "$PID_DIR" "$BIN_DIR"
  if [ ! -f "$SEED_FILE" ]; then
    if command -v openssl >/dev/null 2>&1; then openssl rand -base64 32 > "$SEED_FILE"
    else head -c 32 /dev/urandom | base64 > "$SEED_FILE"; fi
  fi
  export WA_ENV=dev WA_LOG_LEVEL=info
  # Use 127.0.0.1, not "localhost": on macOS localhost resolves to IPv6 ::1
  # first, but the reused Postgres often listens on 127.0.0.1 only, so a Go
  # service's pgx pool spends its whole connect deadline on ::1 and fails with
  # "pg: ping: context deadline exceeded" (psql, which falls back faster, is
  # fine). Pinning IPv4 makes every service's infra connection deterministic.
  export WA_PG_DSN="postgres://whatsapp:devpassword@127.0.0.1:5432/whatsapp?sslmode=disable"
  export WA_VALKEY_ADDR=127.0.0.1:6379
  export WA_NATS_URL=nats://127.0.0.1:4222
  export WA_JWT_ED25519_SEED="$(cat "$SEED_FILE")"
  export WA_OTP_CHANNEL=mock                 # dev: OTP codes are logged, not sent
  # MinIO endpoint uses the LAN IP so the presigned media URLs media-svc mints are
  # reachable from a phone on the same WiFi (localhost:9000 would be the phone
  # itself). MinIO binds 0.0.0.0 (--address :9000). Falls back to localhost.
  export WA_MINIO_ENDPOINT="$(lan_ip):9000" WA_MINIO_ACCESS_KEY=minioadmin \
         WA_MINIO_SECRET_KEY=minioadmin WA_MINIO_BUCKET=media WA_MINIO_SECURE=false
  export WA_CORE_API_GRPC_ADDR=127.0.0.1:9090
  # LiveKit (calls SFU). --dev mode uses fixed placeholder credentials
  # (devkey/secret); core-api mints room join tokens with these so LiveKit
  # accepts them. WA_LIVEKIT_URL uses the LAN IP so a phone on the same WiFi can
  # reach the SFU (the web client derives its own ws:// URL from the API host).
  export WA_LIVEKIT_API_KEY=devkey WA_LIVEKIT_API_SECRET=secret
  export WA_LIVEKIT_URL="ws://$(lan_ip):7880"
}

# svc_port NAME — the listening port a component owns, for status probing.
svc_port() {
  case "$1" in
    core-api) echo 8080 ;; ws-gateway) echo 8081 ;;
    media-svc) echo 8082 ;; notification-svc) echo 8083 ;;
    nats) echo 4222 ;; minio) echo 9000 ;; livekit) echo 7880 ;;
    web) echo "$WEB_PORT" ;; admin) echo "$ADMIN_PORT" ;; mobile) echo "$METRO_PORT" ;;
    *) echo "" ;;
  esac
}

# Per-service HTTP/gRPC ports (distinct so all four share one host).
svc_env() {
  case "$1" in
    core-api)         echo "WA_HTTP_ADDR=:8080 WA_GRPC_ADDR=:9090" ;;
    ws-gateway)       echo "WA_HTTP_ADDR=:8081 WA_GRPC_ADDR=:9091" ;;
    media-svc)        echo "WA_HTTP_ADDR=:8082 WA_GRPC_ADDR=:9092" ;;
    notification-svc) echo "WA_HTTP_ADDR=:8083 WA_GRPC_ADDR=:9093" ;;
  esac
}

# ── infra: docker ───────────────────────────────────────────────────────────
infra_up_docker() {
  say "Starting infra via Docker (PostgreSQL, Valkey, NATS, MinIO)…"
  [ -f "$COMPOSE_ENV" ] || { cp "$REPO_ROOT/deploy/compose/.env.example" "$COMPOSE_ENV"; ok "created deploy/compose/.env"; }
  "${COMPOSE[@]}" up -d
  printf '  waiting for PostgreSQL'
  for _ in $(seq 1 60); do
    if "${COMPOSE[@]}" exec -T postgres pg_isready -U whatsapp -d whatsapp >/dev/null 2>&1; then printf '\n'; ok "infra up"; return 0; fi
    printf '.'; sleep 1
  done
  printf '\n'; die "PostgreSQL did not become ready"
}

migrate_docker() {
  say "Applying migrations (golang-migrate container)…"
  docker run --rm --add-host=host.docker.internal:host-gateway \
    -v "$REPO_ROOT/server/migrations:/migrations" \
    migrate/migrate:v4.18.1 \
    -path=/migrations \
    -database "postgres://whatsapp:devpassword@host.docker.internal:5432/whatsapp?sslmode=disable" up
  ok "migrations applied"
}

# ── infra: native (Homebrew, no Docker) ─────────────────────────────────────
brew_have() { brew list --formula "$1" >/dev/null 2>&1; }
brew_ensure() { brew_have "$1" || { say "Installing $1 (brew)…"; brew install "$1"; }; }

# pg_client echoes a usable psql path (PATH, EDB installer, Postgres.app, brew).
pg_client() {
  command -v psql 2>/dev/null && return 0
  local p
  for p in /Library/PostgreSQL/*/bin/psql /Applications/Postgres.app/Contents/Versions/*/bin/psql \
           "$(brew --prefix postgresql@17 2>/dev/null)/bin/psql" /usr/local/opt/postgresql*/bin/psql; do
    [ -x "$p" ] && { echo "$p"; return 0; }
  done
  return 1
}

# ensure_pg_role_db creates the whatsapp role + db to match the dev DSN. Runs as
# the local superuser (trust auth on localhost); idempotent.
ensure_pg_role_db() {
  local psql="$1"
  "$psql" -h localhost -d postgres -v ON_ERROR_STOP=1 >/dev/null 2>&1 <<'SQL' || warn "role setup skipped (may already exist)"
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='whatsapp') THEN
    CREATE ROLE whatsapp LOGIN PASSWORD 'devpassword' SUPERUSER;
  END IF;
END $$;
SQL
  "$psql" -h localhost -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='whatsapp'" 2>/dev/null | grep -q 1 \
    || "$psql" -h localhost -d postgres -c "CREATE DATABASE whatsapp OWNER whatsapp" >/dev/null 2>&1 || true
}

# pg_up_native reuses an existing Postgres on :5432 (Postgres.app, EDB, …) if one
# is there — the migrations need no PG15+ features — else installs postgresql@17.
pg_up_native() {
  if lsof -nP -iTCP:5432 -sTCP:LISTEN >/dev/null 2>&1; then
    local psql; psql="$(pg_client)" || die "Postgres is on :5432 but no psql client was found to configure it."
    ensure_pg_role_db "$psql"
    ok "Reusing the Postgres already on :5432 (db: whatsapp)"
  else
    brew_ensure postgresql@17
    export PATH="$(brew --prefix postgresql@17)/bin:$PATH"
    brew services start postgresql@17 >/dev/null 2>&1 || true
    printf '  waiting for PostgreSQL'
    for _ in $(seq 1 60); do pg_isready -h localhost -q >/dev/null 2>&1 && break; printf '.'; sleep 1; done; printf '\n'
    ensure_pg_role_db "$(pg_client)"
    ok "PostgreSQL 17 on :5432 (db: whatsapp)"
  fi
}

infra_up_native() {
  say "Starting infra natively (no Docker)…"
  command -v brew >/dev/null 2>&1 || warn "Homebrew not found — can only reuse already-running services (https://brew.sh to install more)."

  pg_up_native

  # Valkey — reuse redis if that's what's installed (wire-compatible)
  if brew_have valkey; then brew services start valkey >/dev/null 2>&1 || true; ok "Valkey on :6379"
  elif brew_have redis; then brew services start redis >/dev/null 2>&1 || true; ok "Redis on :6379 (Valkey-compatible)"
  else brew_ensure valkey; brew services start valkey >/dev/null 2>&1 || true; ok "Valkey on :6379"; fi

  # NATS
  nats_up_native

  # MinIO — object storage for media/backups. Best-effort: if it can't be
  # installed/started (e.g. low disk), we skip it and media-svc, and the rest of
  # the stack still runs.
  minio_up_native || MINIO_OK=0

  # LiveKit — calls SFU (voice/video). Best-effort like MinIO: if it can't
  # start, calls are unavailable but the rest of the stack runs.
  livekit_up_native || warn "LiveKit unavailable — voice/video calls off"
}

# nats_up_native ensures a JETSTREAM-enabled NATS on :4222 — the PUSH durable
# stream (notification-svc) needs it. Crucially we do NOT use `brew services
# start nats-server`: that runs the binary with no args (no -js), so JetStream is
# OFF and the PUSH stream fails with "no responders available". Instead we run
# the binary ourselves with -js, preferring an installed nats-server, else a
# prebuilt release binary (Homebrew source-builds on unsupported macOS). An
# already-running :4222 is reused only if its monitor answers (JetStream on).
nats_up_native() {
  if port_up 4222; then
    if curl -fsS http://localhost:8222/healthz >/dev/null 2>&1; then ok "Reusing NATS (JetStream) on :4222"; return 0; fi
    warn "NATS on :4222 has no JetStream/monitor — replacing it with a -js instance"
    brew services stop nats-server >/dev/null 2>&1 || true
    local held; held="$(lsof -nP -iTCP:4222 -sTCP:LISTEN -t 2>/dev/null | head -1)"
    [ -n "$held" ] && kill "$held" 2>/dev/null || true
  fi
  local bin ver=v2.10.22 tmp
  bin="$(command -v nats-server 2>/dev/null || true)"; [ -x "$bin" ] || bin="$BIN_DIR/nats-server"
  if [ ! -x "$bin" ]; then
    say "Fetching prebuilt nats-server $ver ($OS_ARCH)…"
    tmp="$(mktemp -d)"
    if fetch_bin "https://github.com/nats-io/nats-server/releases/download/${ver}/nats-server-${ver}-darwin-${OS_ARCH}.tar.gz" "$tmp/n.tgz" \
       && tar -xzf "$tmp/n.tgz" -C "$tmp" && mv "$tmp"/nats-server-*/nats-server "$BIN_DIR/nats-server"; then
      chmod +x "$BIN_DIR/nats-server"; bin="$BIN_DIR/nats-server"; rm -rf "$tmp"
    else
      rm -rf "$tmp"; warn "could not fetch nats-server — delivery/presence degraded; install it manually and re-run"; return 1
    fi
  fi
  mkdir -p "$RUN_DIR/nats-js"
  "$bin" -js -sd "$RUN_DIR/nats-js" -m 8222 >"$LOG_DIR/nats.log" 2>&1 &
  echo $! > "$PID_DIR/nats.pid"
  printf '  waiting for NATS (JetStream)'
  curl -fsS --retry 20 --retry-delay 1 --retry-connrefused -o /dev/null http://localhost:8222/healthz 2>/dev/null \
    && { printf '\n'; ok "NATS on :4222 (JetStream)"; } || { printf '\n'; warn "NATS not ready — see ./start.sh logs nats"; return 1; }
}

minio_up_native() {
  local minio_bin mc_bin
  minio_bin="$(command -v minio 2>/dev/null || echo "$BIN_DIR/minio")"
  # No brew install on unsupported macOS (source build) — fetch the prebuilt
  # single-file binary from the official release host instead.
  if [ ! -x "$minio_bin" ]; then
    say "Fetching prebuilt minio ($OS_ARCH)…"
    fetch_bin "https://dl.min.io/server/minio/release/darwin-${OS_ARCH}/minio" "$BIN_DIR/minio" || true
    minio_bin="$BIN_DIR/minio"
  fi
  [ -x "$minio_bin" ] || { warn "minio unavailable — skipping media-svc (media/avatars/backups off)"; return 1; }
  if ! curl -fsS http://localhost:9000/minio/health/live >/dev/null 2>&1; then
    mkdir -p "$MINIO_DATA"
    MINIO_ROOT_USER=minioadmin MINIO_ROOT_PASSWORD=minioadmin \
      "$minio_bin" server "$MINIO_DATA" --address :9000 --console-address :9001 >"$LOG_DIR/minio.log" 2>&1 &
    echo $! > "$PID_DIR/minio.pid"
    printf '  waiting for MinIO'
    for _ in $(seq 1 30); do curl -fsS http://localhost:9000/minio/health/live >/dev/null 2>&1 && break; printf '.'; sleep 1; done; printf '\n'
  fi
  curl -fsS http://localhost:9000/minio/health/live >/dev/null 2>&1 || { warn "MinIO did not become ready — skipping media-svc"; return 1; }
  # Buckets the services expect (media-svc, backups). Best-effort via mc (prebuilt).
  mc_bin="$(command -v mc 2>/dev/null || echo "$BIN_DIR/mc")"
  [ -x "$mc_bin" ] || fetch_bin "https://dl.min.io/client/mc/release/darwin-${OS_ARCH}/mc" "$BIN_DIR/mc" || true
  mc_bin="$(command -v mc 2>/dev/null || echo "$BIN_DIR/mc")"
  if [ -x "$mc_bin" ]; then
    "$mc_bin" alias set wa-local http://localhost:9000 minioadmin minioadmin >/dev/null 2>&1 || true
    for b in media backups wal-archive; do "$mc_bin" mb -p "wa-local/$b" >/dev/null 2>&1 || true; done
  fi
  ok "MinIO on :9000 (console :9001, minioadmin/minioadmin)"
}

# livekit_up_native ensures a LiveKit SFU on :7880 in --dev mode. --dev uses the
# fixed placeholder credentials devkey/secret, which match load_env's
# WA_LIVEKIT_* so core-api's minted room tokens verify. It binds 0.0.0.0 (:7880
# signal + UDP 7882+ for WebRTC) so a phone on the same WiFi can reach it. No
# prebuilt fetch: LiveKit ships per-arch archives — if it isn't installed we skip
# (calls off) with an install hint rather than guessing a download.
livekit_up_native() {
  if port_up 7880; then ok "Reusing LiveKit on :7880"; return 0; fi
  local bin; bin="$(command -v livekit-server 2>/dev/null || true)"
  [ -x "$bin" ] || { warn "livekit-server not found — 'brew install livekit' (or livekit.io/downloads) to enable calls"; return 1; }
  "$bin" --dev --bind 0.0.0.0 >"$LOG_DIR/livekit.log" 2>&1 &
  echo $! > "$PID_DIR/livekit.pid"
  printf '  waiting for LiveKit'
  # -S keeps curl's error text on stderr, which used to smear across the
  # progress dots ("waiting for LiveKitcurl: (7) Failed to connect…").
  for _ in $(seq 1 20); do curl -fsS http://localhost:7880 -o /dev/null >/dev/null 2>&1 && break; printf '.'; sleep 1; done; printf '\n'
  curl -fsS http://localhost:7880 -o /dev/null >/dev/null 2>&1 && ok "LiveKit on :7880 (dev)" || { warn "LiveKit not ready — see ./start.sh logs livekit"; return 1; }
}

migrate_native() {
  say "Applying migrations…"
  if command -v migrate >/dev/null 2>&1; then
    migrate -path "$REPO_ROOT/server/migrations" -database "$WA_PG_DSN" up
    ok "migrations applied"; return 0
  fi
  # No golang-migrate — apply *.up.sql via psql, tracked in a tiny ledger so
  # re-runs are IDEMPOTENT (the plain psql path has no schema_migrations table,
  # so without this a second run re-applies 000002 and hits "users already
  # exists"). Migrations use no PG15+ features, so psql is a safe applier.
  local psql; psql="$(pg_client)" || die "no psql client found to apply migrations"
  local q=(env PGPASSWORD=devpassword "$psql" -h localhost -U whatsapp -d whatsapp -v ON_ERROR_STOP=1 -q)
  "${q[@]}" -c "CREATE TABLE IF NOT EXISTS _startsh_migrations (filename text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())" >/dev/null \
    || die "could not create the migration ledger"
  # Bootstrap: a DB migrated before this ledger existed (e.g. an earlier run of
  # this script) already carries the full schema — record every file as applied
  # so we don't try to re-run it. The psql applier is all-or-nothing, so a
  # present `users` table means the prior run completed.
  local count sentinel f base
  count="$("${q[@]}" -tAc "SELECT count(*) FROM _startsh_migrations" | tr -d '[:space:]')"
  sentinel="$("${q[@]}" -tAc "SELECT (to_regclass('public.users') IS NOT NULL)" | tr -d '[:space:]')"
  if [ "$count" = "0" ] && [ "$sentinel" = "t" ]; then
    for f in "$REPO_ROOT"/server/migrations/*.up.sql; do
      "${q[@]}" -c "INSERT INTO _startsh_migrations(filename) VALUES ('$(basename "$f")') ON CONFLICT DO NOTHING" >/dev/null
    done
    warn "existing schema detected — recorded current migrations as applied (no re-run)"
  fi
  for f in $(ls "$REPO_ROOT"/server/migrations/*.up.sql | sort); do
    base="$(basename "$f")"
    [ "$("${q[@]}" -tAc "SELECT 1 FROM _startsh_migrations WHERE filename='$base'" | tr -d '[:space:]')" = "1" ] && continue
    "${q[@]}" -f "$f" >/dev/null || die "migration failed at $base — see the error above"
    "${q[@]}" -c "INSERT INTO _startsh_migrations(filename) VALUES ('$base') ON CONFLICT DO NOTHING" >/dev/null
  done
  ok "migrations applied"
}

# ── protobuf codegen ────────────────────────────────────────────────────────
# Generated code (server/internal/proto/gen + clients/packages/proto-types/src/gen)
# is deliberately NEVER committed — see .gitignore: "CI runs buf generate before
# every Go compile", which is exactly what ci.yml does (buf-action → buf generate
# → go build). start.sh did not, so on any fresh clone or new git worktree the
# build died with "no required module provides package .../internal/proto/gen/…"
# and the script exited BEFORE starting a single service, let alone a UI. This is
# that missing step.
proto_gen() {
  local go_gen="$REPO_ROOT/server/internal/proto/gen"
  local ts_gen="$REPO_ROOT/clients/packages/proto-types/src/gen"
  # Skip when nothing changed: buf resolves remote plugins over the network, so
  # a needless run costs a round-trip to the BSR on every start.
  if [ -d "$go_gen" ] && [ -d "$ts_gen" ] && [ -f "$PROTO_STAMP" ] \
     && [ -z "$(find "$REPO_ROOT/server/proto" -name '*.proto' -newer "$PROTO_STAMP" -print -quit 2>/dev/null)" ]; then
    ok "protobuf code up to date"
    return 0
  fi
  local buf; buf="$(command -v buf 2>/dev/null || true)"
  [ -x "$buf" ] || buf="$BIN_DIR/buf"
  if [ ! -x "$buf" ]; then
    say "Fetching prebuilt buf $BUF_VER ($BUF_ARCH)…"
    fetch_bin "https://github.com/bufbuild/buf/releases/download/${BUF_VER}/buf-Darwin-${BUF_ARCH}" "$BIN_DIR/buf" || true
    buf="$BIN_DIR/buf"
  fi
  if [ ! -x "$buf" ]; then
    [ -d "$go_gen" ] && { warn "buf unavailable — reusing the existing generated code (may be stale)"; return 0; }
    die "buf could not be fetched and there is no generated protobuf code, so the Go services cannot build. Install it (brew install bufbuild/buf/buf) or run 'make proto', then re-run."
  fi
  say "Generating protobuf code (buf → Go + TypeScript)…"
  if ( cd "$REPO_ROOT/server/proto" && "$buf" generate ) >"$LOG_DIR/proto.log" 2>&1; then
    touch "$PROTO_STAMP"; ok "protobuf code generated"; return 0
  fi
  warn "buf generate failed — last lines of $LOG_DIR/proto.log:"
  tail -n 15 "$LOG_DIR/proto.log" | sed 's/^/      /'
  [ -d "$go_gen" ] && { warn "continuing with the existing generated code"; return 0; }
  die "codegen failed and no generated code exists — the Go services cannot build. buf needs network access to buf.build for its remote plugins."
}

# ── Go services ─────────────────────────────────────────────────────────────
build_services() {
  proto_gen
  say "Building Go services…"
  ( cd "$REPO_ROOT/server" && for s in "${SERVICES[@]}"; do go build -o "$BIN_DIR/$s" "./cmd/$s"; done )
  ok "built: ${SERVICES[*]}"
}

start_service() {
  local s="$1"
  if running "$s"; then warn "$s already running (pid $(cat "$PID_DIR/$s.pid"))"; return 0; fi
  # shellcheck disable=SC2046
  env $(svc_env "$s") "$BIN_DIR/$s" >"$LOG_DIR/$s.log" 2>&1 &
  echo $! > "$PID_DIR/$s.pid"
  ok "$s started (pid $!) → $LOG_DIR/$s.log"
}

wait_core_api() {
  printf '  waiting for core-api'
  for _ in $(seq 1 30); do curl -fsS http://localhost:8080/readyz >/dev/null 2>&1 && { printf '\n'; ok "core-api ready"; return 0; }; printf '.'; sleep 1; done
  printf '\n'; warn "core-api /readyz not green yet — check ./start.sh logs core-api"
}

services_up() {
  build_services
  say "Starting services…"
  start_service core-api
  wait_core_api
  start_service ws-gateway
  if [ "$MINIO_OK" = 1 ]; then start_service media-svc; else warn "media-svc not started (MinIO unavailable)"; fi
  start_service notification-svc
}

# ── UIs: web PWA, admin console, mobile (Expo/Metro) ────────────────────────
# All three front-ends come up with ./start.sh. Each one installs from the same
# workspace, is health-probed before it is called "started", and reports its own
# log on failure instead of leaving a URL pointing at nothing.

DEPS_OK=0   # set by deps_install; the UIs refuse to start without it

# deps_install runs ONE workspace install for web + admin + mobile. Failures are
# no longer swallowed: a broken install used to surface as a dev server that
# simply never came up, with the reason discarded to /dev/null.
deps_install() {
  command -v pnpm >/dev/null 2>&1 || { warn "pnpm not found — skipping all UIs (backend is still up). Install Node + pnpm: https://pnpm.io/installation"; return 1; }
  say "Installing client dependencies (pnpm workspace)…"
  if ( cd "$REPO_ROOT/clients" && pnpm install --no-frozen-lockfile ) >"$LOG_DIR/pnpm-install.log" 2>&1; then
    DEPS_OK=1; ok "client dependencies ready"; return 0
  fi
  warn "pnpm install failed — UIs cannot start. Last lines of $LOG_DIR/pnpm-install.log:"
  tail -n 15 "$LOG_DIR/pnpm-install.log" | sed 's/^/      /'
  return 1
}

# vite_app_up NAME DIR PORT URL — start a Vite dev server and PROVE it serves.
# Binds 0.0.0.0 so a phone on the same WiFi can load it. NOTE: vite is invoked
# directly — `pnpm dev -- --host` passes a stray `--` that vite reads as
# end-of-options, so --host is silently ignored and the LAN never sees it.
vite_app_up() {
  local name="$1" dir="$2" port="$3" url="$4"
  if running "$name"; then warn "$name already running (pid $(cat "$PID_DIR/$name.pid"))"; return 0; fi
  # --strictPort means vite exits rather than drifting to another port, so a
  # squatter on this port would otherwise leave us advertising someone else's app.
  if port_up "$port"; then
    local holder; holder="$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null | head -1)"
    if curl -fsS -o /dev/null --max-time 2 "$url" >/dev/null 2>&1; then
      warn "$name: $url is already being served by pid ${holder:-?} (started outside this script)."
      warn "     It may be stale and bound to localhost only — 'kill ${holder:-PID}' then './start.sh ui' to rebind on the LAN."
    else
      warn "$name: port $port is held by pid ${holder:-?} but nothing answers there — free it and re-run './start.sh ui'."
    fi
    return 1
  fi
  ( cd "$dir" && exec pnpm exec vite --host 0.0.0.0 --port "$port" --strictPort ) >"$LOG_DIR/$name.log" 2>&1 &
  echo $! > "$PID_DIR/$name.pid"
  printf '  waiting for %s' "$name"
  if http_wait "$url" 40; then printf '\n'; ok "$name → $url"; return 0; fi
  printf '\n'
  warn "$name did not come up. Last lines of $LOG_DIR/$name.log:"
  tail -n 15 "$LOG_DIR/$name.log" | sed 's/^/      /'
  rm -f "$PID_DIR/$name.pid"
  return 1
}

web_up() {
  [ "${WA_SKIP_WEB:-0}" = 1 ] && { warn "web skipped (WA_SKIP_WEB=1)"; return 0; }
  [ "$DEPS_OK" = 1 ] || return 0
  say "Starting web app (Vite)…"
  local ip; ip="$(lan_ip)"
  # Point the web app at the LAN IP (not localhost) so it also works when opened
  # from a phone browser on the same WiFi; regenerated each run to track the IP.
  printf 'VITE_API_URL=http://%s:8080\nVITE_WS_URL=ws://%s:8081/v1/ws\n' "$ip" "$ip" > "$REPO_ROOT/clients/web/.env.local"
  vite_app_up web "$REPO_ROOT/clients/web" "$WEB_PORT" "http://localhost:$WEB_PORT/"
}

# admin_up starts the internal admin console (clients/admin). It derives its own
# API base from the browser host on :8080, so it needs no env. The server-side
# admin plane is OIDC-gated (core-api/main.go: WA_ADMIN_OIDC_ISSUER + _JWKS —
# unset means the /admin/v1 routes are never mounted), so say that plainly here
# rather than handing over a console whose every request 404s for no visible
# reason.
admin_up() {
  [ "${WA_SKIP_ADMIN:-0}" = 1 ] && { warn "admin console skipped (WA_SKIP_ADMIN=1)"; return 0; }
  [ "$DEPS_OK" = 1 ] || return 0
  say "Starting admin console (Vite)…"
  vite_app_up admin "$REPO_ROOT/clients/admin" "$ADMIN_PORT" "http://localhost:$ADMIN_PORT/" || return 1
  if [ -z "${WA_ADMIN_OIDC_ISSUER:-}" ] || [ -z "${WA_ADMIN_OIDC_JWKS:-}" ]; then
    warn "admin plane is DISABLED server-side (WA_ADMIN_OIDC_ISSUER/_JWKS unset) — the console loads but /admin/v1 is not mounted"
  fi
}

# mobile_up starts the Expo dev server (Metro) for the React Native app. The
# EXPO_PUBLIC_* values are baked into the bundle at build time and must be the
# LAN IP, never localhost: on a phone, localhost is the phone. Metro is pinned
# to $METRO_PORT because Expo's default (8081) is ws-gateway here.
#
# EXPO_NO_TYPESCRIPT_SETUP=1 is load-bearing: without it every `expo start`
# rewrites clients/mobile/tsconfig.json and DELETES the tracked
# clients/mobile/expo-env.d.ts, so merely bringing the stack up left the working
# tree dirty. Starting a dev server must never edit the repo.
mobile_up() {
  [ "${WA_SKIP_MOBILE:-0}" = 1 ] && { warn "mobile/Expo skipped (WA_SKIP_MOBILE=1)"; return 0; }
  [ "$DEPS_OK" = 1 ] || return 0
  if running mobile; then warn "mobile already running (pid $(cat "$PID_DIR/mobile.pid"))"; return 0; fi
  if port_up "$METRO_PORT"; then warn "mobile: port $METRO_PORT already in use — Metro not started"; return 1; fi
  say "Starting mobile dev server (Expo/Metro)…"
  local ip; ip="$(lan_ip)"
  (
    cd "$REPO_ROOT/clients/mobile" \
      && EXPO_NO_TELEMETRY=1 \
         EXPO_NO_TYPESCRIPT_SETUP=1 \
         EXPO_PUBLIC_API_URL="http://$ip:8080" \
         EXPO_PUBLIC_WS_URL="ws://$ip:8081/v1/ws" \
         EXPO_PUBLIC_LIVEKIT_URL="ws://$ip:7880" \
         exec pnpm exec expo start --port "$METRO_PORT"
  ) >"$LOG_DIR/mobile.log" 2>&1 &
  echo $! > "$PID_DIR/mobile.pid"
  printf '  waiting for Metro'
  # Metro's first boot compiles the dep graph — give it longer than a Vite start.
  if http_wait "http://localhost:$METRO_PORT/status" 90; then
    printf '\n'; ok "mobile (Metro) → exp://$ip:$METRO_PORT  (open in Expo Go)"
    return 0
  fi
  printf '\n'
  warn "Metro did not come up. Last lines of $LOG_DIR/mobile.log:"
  tail -n 15 "$LOG_DIR/mobile.log" | sed 's/^/      /'
  rm -f "$PID_DIR/mobile.pid"
  return 1
}

# uis_up brings up every front-end. Ordered cheapest-first so a slow Metro boot
# never delays the browser UIs.
uis_up() {
  proto_gen              # @wa/proto-types is generated, not committed
  deps_install || return 0
  web_up   || true
  admin_up || true
  mobile_up || true
}

# open_browser opens the web app once it is actually serving — the payoff of
# "run everything". WA_NO_OPEN=1 suppresses it (CI, headless, tmux).
open_browser() {
  [ "${WA_NO_OPEN:-0}" = 1 ] && return 0
  running web || return 0
  command -v open >/dev/null 2>&1 && open "http://localhost:$WEB_PORT" >/dev/null 2>&1 || true
}

# ── lifecycle helpers ───────────────────────────────────────────────────────
running() { local p="$PID_DIR/$1.pid"; [ -f "$p" ] && kill -0 "$(cat "$p")" 2>/dev/null; }

stop_procs() {
  say "Stopping host processes…"
  [ -d "$PID_DIR" ] || return 0
  for p in "$PID_DIR"/*.pid; do
    [ -e "$p" ] || continue
    local name pid; name="$(basename "$p" .pid)"; pid="$(cat "$p")"
    # kill_tree, not kill: the UI dev servers fork children (Vite's optimizer,
    # Metro's transform workers) that survive a kill of the parent and keep the
    # port bound, so the next `up` finds 5173/8090 "already in use".
    if kill -0 "$pid" 2>/dev/null; then kill_tree "$pid"; ok "stopped $name (pid $pid)"; fi
    rm -f "$p"
  done
}

infra_down_native() {
  say "Stopping native infra (brew services)…"
  for svc in postgresql@17 valkey redis nats-server; do
    brew_have "$svc" && brew services stop "$svc" >/dev/null 2>&1 && ok "stopped $svc" || true
  done
}

urls() {
  local ip; ip="$(lan_ip)"
  cat <<EOF

${B}Stack is up (${RUNTIME}).${X}
  ${D}core-api      ${X}http://localhost:8080  (readyz: /readyz, metrics: /metrics)
  ${D}ws-gateway    ${X}ws://localhost:8081/v1/ws
  ${D}media-svc     ${X}http://localhost:8082
  ${D}notify-svc    ${X}http://localhost:8083
  ${D}LiveKit SFU   ${X}ws://localhost:7880    (voice/video)
  ${D}MinIO console ${X}http://localhost:9001  (minioadmin / minioadmin)
  ${D}Postgres      ${X}localhost:5432         (whatsapp / devpassword)

${B}User interfaces.${X}
  ${D}web app       ${X}http://localhost:${WEB_PORT}  (LAN: http://${ip}:${WEB_PORT})
  ${D}admin console ${X}http://localhost:${ADMIN_PORT}  (LAN: http://${ip}:${ADMIN_PORT})
  ${D}mobile (Expo) ${X}exp://${ip}:${METRO_PORT}    (open in Expo Go; Metro on :${METRO_PORT})

  ${B}📱 Phone on the same WiFi${X} — the mobile bundle is already built with:
       API   http://${ip}:8080
       WS    ws://${ip}:8081/v1/ws
     For the web PWA in a phone browser, just open http://${ip}:${WEB_PORT}.
     (services already bind 0.0.0.0; if the phone can't reach it, allow incoming
      connections in System Settings ▸ Network ▸ Firewall.)

  Logs:  ./start.sh logs web           Stop:  ./start.sh down
  UIs only:  ./start.sh ui             Skip one:  WA_SKIP_MOBILE=1 ./start.sh
EOF
}

# ── commands ────────────────────────────────────────────────────────────────
cmd_up() {
  need go
  detect_runtime
  load_env
  if [ "$RUNTIME" = docker ]; then infra_up_docker; migrate_docker
  else infra_up_native; migrate_native; fi
  services_up
  uis_up
  urls
  open_browser
}

# cmd_ui (re)starts only the front-ends, against a backend that is already up —
# the common case when you are iterating on a client.
cmd_ui() {
  detect_runtime
  load_env
  uis_up
  urls
  open_browser
}

cmd_down() {
  detect_runtime
  stop_procs
  if [ "$RUNTIME" = docker ] && command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    say "Stopping Docker infra…"; "${COMPOSE[@]}" down && ok "infra stopped (data kept)"
  else
    infra_down_native
  fi
}

cmd_status() {
  detect_runtime
  say "Runtime: $RUNTIME"
  say "Host processes:"
  local port
  for s in "${SERVICES[@]}" nats minio livekit "${UI_APPS[@]}"; do
    port="$(svc_port "$s")"
    if running "$s"; then
      ok "$s (pid $(cat "$PID_DIR/$s.pid"), :$port)"
    elif [ -n "$port" ] && port_up "$port"; then
      # Up, but not ours — start.sh reuses an instance that was already
      # listening (Postgres, NATS, MinIO, LiveKit) and writes no pidfile for it.
      ok "$s (:$port, reused — not started by this script)"
    else
      printf '  %s·%s %s stopped\n' "$D" "$X" "$s"
    fi
  done
  if [ "$RUNTIME" = docker ] && docker info >/dev/null 2>&1; then say "Infra (docker):"; "${COMPOSE[@]}" ps
  else say "Infra (brew services):"; brew services list 2>/dev/null | grep -E "postgresql@17|valkey|redis|nats-server" || true; fi
}

cmd_logs() {
  local s="${1:-core-api}"; local f="$LOG_DIR/$s.log"
  [ -f "$f" ] || die "no log for '$s' (choose: ${SERVICES[*]} ${UI_APPS[*]} nats minio livekit pnpm-install)"
  tail -f "$f"
}

case "${1:-up}" in
  up)      RUNTIME="${2:-}"; cmd_up ;;
  native)  RUNTIME=native; cmd_up ;;
  docker)  RUNTIME=docker; cmd_up ;;
  ui)      cmd_ui ;;
  down)    cmd_down ;;
  restart) cmd_down; cmd_up ;;
  status)  cmd_status ;;
  logs)    cmd_logs "${2:-}" ;;
  *)       die "usage: ./start.sh [up [native|docker]|ui|down|restart|status|logs <service>]" ;;
esac
