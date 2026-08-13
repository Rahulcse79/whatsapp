#!/usr/bin/env bash
#
# start.sh — bring up the whole WhatsApp V2 stack on one box for local dev.
#
#   ./start.sh            # up: infra → migrations → 4 Go services → web
#   ./start.sh up [native|docker]
#   ./start.sh down       # stop the Go services + web + infra
#   ./start.sh restart
#   ./start.sh status
#   ./start.sh logs [svc] # core-api|ws-gateway|media-svc|notification-svc|web|minio
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
COMPOSE=(docker compose -f "$COMPOSE_FILE")
RUNTIME=""
MINIO_OK=1   # media-svc is started only when MinIO is available

# ── pretty output ───────────────────────────────────────────────────────────
if [ -t 1 ]; then B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; D=$'\033[2m'; X=$'\033[0m'; else B= G= Y= R= D= X=; fi
say()  { printf '%s▸ %s%s\n' "$B" "$*" "$X"; }
ok()   { printf '  %s✓%s %s\n' "$G" "$X" "$*"; }
warn() { printf '  %s!%s %s\n' "$Y" "$X" "$*"; }
die()  { printf '%s✗ %s%s\n' "$R" "$*" "$X" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing prerequisite: $1"; }

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
  export WA_PG_DSN="postgres://whatsapp:devpassword@localhost:5432/whatsapp?sslmode=disable"
  export WA_VALKEY_ADDR=localhost:6379
  export WA_NATS_URL=nats://localhost:4222
  export WA_JWT_ED25519_SEED="$(cat "$SEED_FILE")"
  export WA_OTP_CHANNEL=mock                 # dev: OTP codes are logged, not sent
  export WA_MINIO_ENDPOINT=localhost:9000 WA_MINIO_ACCESS_KEY=minioadmin \
         WA_MINIO_SECRET_KEY=minioadmin WA_MINIO_BUCKET=media WA_MINIO_SECURE=false
  export WA_CORE_API_GRPC_ADDR=localhost:9090
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
  brew_ensure nats-server
  brew services start nats-server >/dev/null 2>&1 || true
  ok "NATS on :4222"

  # MinIO — object storage for media/backups. Best-effort: if it can't be
  # installed/started (e.g. low disk), we skip it and media-svc, and the rest of
  # the stack still runs.
  minio_up_native || MINIO_OK=0
}

minio_up_native() {
  if ! command -v minio >/dev/null 2>&1; then
    command -v brew >/dev/null 2>&1 && { say "Installing minio (brew)…"; brew install minio >/dev/null 2>&1 || true; }
  fi
  command -v minio >/dev/null 2>&1 || { warn "minio unavailable — skipping media-svc (media/avatars/backups off)"; return 1; }
  if ! curl -fsS http://localhost:9000/minio/health/live >/dev/null 2>&1; then
    mkdir -p "$MINIO_DATA"
    MINIO_ROOT_USER=minioadmin MINIO_ROOT_PASSWORD=minioadmin \
      minio server "$MINIO_DATA" --address :9000 --console-address :9001 >"$LOG_DIR/minio.log" 2>&1 &
    echo $! > "$PID_DIR/minio.pid"
    printf '  waiting for MinIO'
    for _ in $(seq 1 30); do curl -fsS http://localhost:9000/minio/health/live >/dev/null 2>&1 && break; printf '.'; sleep 1; done; printf '\n'
  fi
  curl -fsS http://localhost:9000/minio/health/live >/dev/null 2>&1 || { warn "MinIO did not become ready — skipping media-svc"; return 1; }
  # Buckets the services expect (media-svc, backups). Best-effort via mc.
  command -v mc >/dev/null 2>&1 || brew install minio-mc >/dev/null 2>&1 || true
  if command -v mc >/dev/null 2>&1; then
    mc alias set wa-local http://localhost:9000 minioadmin minioadmin >/dev/null 2>&1 || true
    for b in media backups wal-archive; do mc mb -p "wa-local/$b" >/dev/null 2>&1 || true; done
  fi
  ok "MinIO on :9000 (console :9001, minioadmin/minioadmin)"
}

migrate_native() {
  say "Applying migrations…"
  if command -v migrate >/dev/null 2>&1; then
    migrate -path "$REPO_ROOT/server/migrations" -database "$WA_PG_DSN" up
  else
    # No golang-migrate installed — apply *.up.sql in order via psql (fresh dev
    # DB; migrations use no PG15+ features, so this is safe). Re-running is fine:
    # 000001 uses IF NOT EXISTS, later files fail only if already applied, which
    # we surface. For a clean re-run use ./start.sh down then a fresh DB.
    local psql; psql="$(pg_client)" || die "no psql client found to apply migrations"
    local f
    for f in $(ls "$REPO_ROOT"/server/migrations/*.up.sql | sort); do
      PGPASSWORD=devpassword "$psql" -h localhost -U whatsapp -d whatsapp -v ON_ERROR_STOP=1 -q -f "$f" >/dev/null \
        || die "migration failed at $(basename "$f") — see the error above"
    done
  fi
  ok "migrations applied"
}

# ── Go services ─────────────────────────────────────────────────────────────
build_services() {
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

# ── web (optional) ──────────────────────────────────────────────────────────
web_up() {
  command -v pnpm >/dev/null 2>&1 || { warn "pnpm not found — skipping the web app (backend is still up)"; return 0; }
  say "Starting web app (Vite)…"
  local webenv="$REPO_ROOT/clients/web/.env.local"
  [ -f "$webenv" ] || printf 'VITE_API_URL=http://localhost:8080\nVITE_WS_URL=ws://localhost:8081/v1/ws\n' > "$webenv"
  ( cd "$REPO_ROOT/clients" && pnpm install --no-frozen-lockfile >/dev/null 2>&1 || true )
  ( cd "$REPO_ROOT/clients/web" && exec pnpm dev ) >"$LOG_DIR/web.log" 2>&1 &
  echo $! > "$PID_DIR/web.pid"
  ok "web starting → http://localhost:5173  ($LOG_DIR/web.log)"
}

# ── lifecycle helpers ───────────────────────────────────────────────────────
running() { local p="$PID_DIR/$1.pid"; [ -f "$p" ] && kill -0 "$(cat "$p")" 2>/dev/null; }

stop_procs() {
  say "Stopping host processes…"
  [ -d "$PID_DIR" ] || return 0
  for p in "$PID_DIR"/*.pid; do
    [ -e "$p" ] || continue
    local name pid; name="$(basename "$p" .pid)"; pid="$(cat "$p")"
    if kill -0 "$pid" 2>/dev/null; then kill "$pid" 2>/dev/null || true; ok "stopped $name (pid $pid)"; fi
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
  cat <<EOF

${B}Stack is up (${RUNTIME}).${X}
  ${D}core-api     ${X}http://localhost:8080   (readyz: /readyz, metrics: /metrics)
  ${D}ws-gateway   ${X}ws://localhost:8081/v1/ws
  ${D}media-svc    ${X}http://localhost:8082
  ${D}notify-svc   ${X}http://localhost:8083
  ${D}web app      ${X}http://localhost:5173
  ${D}MinIO console${X}http://localhost:9001    (minioadmin / minioadmin)
  ${D}Postgres     ${X}localhost:5432           (whatsapp / devpassword)

  Logs:  ./start.sh logs core-api      Stop:  ./start.sh down
  Phone/emulator: point the app's API base at http://<this-machine-ip>:8080 (see .github/ANDROID.md).
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
  web_up
  urls
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
  for s in "${SERVICES[@]}" minio web; do
    if running "$s"; then ok "$s (pid $(cat "$PID_DIR/$s.pid"))"; else printf '  %s·%s %s stopped\n' "$D" "$X" "$s"; fi
  done
  if [ "$RUNTIME" = docker ] && docker info >/dev/null 2>&1; then say "Infra (docker):"; "${COMPOSE[@]}" ps
  else say "Infra (brew services):"; brew services list 2>/dev/null | grep -E "postgresql@17|valkey|redis|nats-server" || true; fi
}

cmd_logs() {
  local s="${1:-core-api}"; local f="$LOG_DIR/$s.log"
  [ -f "$f" ] || die "no log for '$s' (choose: ${SERVICES[*]} web minio)"
  tail -f "$f"
}

case "${1:-up}" in
  up)      RUNTIME="${2:-}"; cmd_up ;;
  native)  RUNTIME=native; cmd_up ;;
  docker)  RUNTIME=docker; cmd_up ;;
  down)    cmd_down ;;
  restart) cmd_down; cmd_up ;;
  status)  cmd_status ;;
  logs)    cmd_logs "${2:-}" ;;
  *)       die "usage: ./start.sh [up [native|docker]|down|restart|status|logs <service>]" ;;
esac
