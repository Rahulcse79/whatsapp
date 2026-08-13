#!/usr/bin/env bash
#
# start.sh — bring up the whole WhatsApp V2 stack on one box for local dev.
#
#   ./start.sh            # up:   infra (compose) → migrations → 4 Go services → web
#   ./start.sh up         # same as above
#   ./start.sh down       # stop the Go services + web, then the compose infra
#   ./start.sh restart    # down then up
#   ./start.sh status     # show what's running + the URLs
#   ./start.sh logs [svc] # tail a service log (core-api|ws-gateway|media-svc|notification-svc|web)
#
# Infra runs in Docker (deploy/compose); the Go services + web run on the host
# against it (the dev config defaults already point at localhost). Everything is
# best-effort and idempotent — re-running `up` is safe.
#
# Prereqs: docker (+ compose v2), go 1.25. Optional: pnpm/node (for the web app).
set -euo pipefail

# ── paths ──────────────────────────────────────────────────────────────────
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$REPO_ROOT/deploy/compose/docker-compose.yml"
COMPOSE_ENV="$REPO_ROOT/deploy/compose/.env"
RUN_DIR="$REPO_ROOT/.run"
LOG_DIR="$RUN_DIR/logs"
PID_DIR="$RUN_DIR/pids"
BIN_DIR="$RUN_DIR/bin"
SEED_FILE="$RUN_DIR/jwt-seed"

SERVICES=(core-api ws-gateway media-svc notification-svc)
COMPOSE=(docker compose -f "$COMPOSE_FILE")

# ── pretty output ───────────────────────────────────────────────────────────
if [ -t 1 ]; then B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; D=$'\033[2m'; X=$'\033[0m'; else B= G= Y= R= D= X=; fi
say()  { printf '%s▸ %s%s\n' "$B" "$*" "$X"; }
ok()   { printf '  %s✓%s %s\n' "$G" "$X" "$*"; }
warn() { printf '  %s!%s %s\n' "$Y" "$X" "$*"; }
die()  { printf '%s✗ %s%s\n' "$R" "$*" "$X" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "missing prerequisite: $1"; }

# ── shared environment for every Go service ─────────────────────────────────
# One JWT seed is generated once and reused, so tokens minted by core-api verify
# in ws-gateway (and survive restarts). Dev config defaults cover PG/Valkey/NATS.
load_env() {
  mkdir -p "$RUN_DIR"
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

# ── infra (compose) ─────────────────────────────────────────────────────────
infra_up() {
  say "Starting infra (PostgreSQL, Valkey, NATS, MinIO)…"
  [ -f "$COMPOSE_ENV" ] || { cp "$REPO_ROOT/deploy/compose/.env.example" "$COMPOSE_ENV"; ok "created deploy/compose/.env from the example"; }
  "${COMPOSE[@]}" up -d
  printf '  waiting for PostgreSQL'
  for _ in $(seq 1 60); do
    if "${COMPOSE[@]}" exec -T postgres pg_isready -U whatsapp -d whatsapp >/dev/null 2>&1; then
      printf '\n'; ok "infra is up"; return 0
    fi
    printf '.'; sleep 1
  done
  printf '\n'; die "PostgreSQL did not become ready — check: ./start.sh logs, or docker compose -f $COMPOSE_FILE ps"
}

# ── migrations (golang-migrate in a container, same image CI uses) ──────────
migrate() {
  say "Applying database migrations…"
  # A one-off container reaches the host-published Postgres via host.docker.internal
  # (mapped explicitly so it also works on native-Linux Docker).
  docker run --rm \
    --add-host=host.docker.internal:host-gateway \
    -v "$REPO_ROOT/server/migrations:/migrations" \
    migrate/migrate:v4.18.1 \
    -path=/migrations \
    -database "postgres://whatsapp:devpassword@host.docker.internal:5432/whatsapp?sslmode=disable" \
    up
  ok "migrations applied"
}

# ── Go services ─────────────────────────────────────────────────────────────
build_services() {
  say "Building Go services…"
  mkdir -p "$BIN_DIR" "$LOG_DIR" "$PID_DIR"
  ( cd "$REPO_ROOT/server" && for s in "${SERVICES[@]}"; do
      go build -o "$BIN_DIR/$s" "./cmd/$s"
    done )
  ok "built: ${SERVICES[*]}"
}

start_service() {
  local s="$1"; local pidf="$PID_DIR/$s.pid"
  if running "$s"; then warn "$s already running (pid $(cat "$pidf"))"; return 0; fi
  # shellcheck disable=SC2046
  env $(svc_env "$s") "$BIN_DIR/$s" >"$LOG_DIR/$s.log" 2>&1 &
  echo $! > "$pidf"
  ok "$s started (pid $!) → $LOG_DIR/$s.log"
}

wait_core_api() {
  printf '  waiting for core-api'
  for _ in $(seq 1 30); do
    if curl -fsS http://localhost:8080/readyz >/dev/null 2>&1; then printf '\n'; ok "core-api ready"; return 0; fi
    printf '.'; sleep 1
  done
  printf '\n'; warn "core-api /readyz not green yet — dependents may retry (see logs)"
}

services_up() {
  build_services
  say "Starting services…"
  start_service core-api
  wait_core_api                       # ws-gateway + media-svc dial core-api's gRPC
  for s in ws-gateway media-svc notification-svc; do start_service "$s"; done
}

# ── web (optional) ──────────────────────────────────────────────────────────
web_up() {
  command -v pnpm >/dev/null 2>&1 || { warn "pnpm not found — skipping the web app (backend is still up)"; return 0; }
  say "Starting web app (Vite)…"
  local webenv="$REPO_ROOT/clients/web/.env.local"
  [ -f "$webenv" ] || printf 'VITE_API_URL=http://localhost:8080\nVITE_WS_URL=ws://localhost:8081\n' > "$webenv"
  ( cd "$REPO_ROOT/clients" && pnpm install --no-frozen-lockfile >/dev/null 2>&1 || true )
  ( cd "$REPO_ROOT/clients/web" && exec pnpm dev ) >"$LOG_DIR/web.log" 2>&1 &
  echo $! > "$PID_DIR/web.pid"
  ok "web starting → http://localhost:5173  ($LOG_DIR/web.log)"
}

# ── lifecycle helpers ───────────────────────────────────────────────────────
running() { local p="$PID_DIR/$1.pid"; [ -f "$p" ] && kill -0 "$(cat "$p")" 2>/dev/null; }

stop_procs() {
  say "Stopping host processes…"
  for p in "$PID_DIR"/*.pid; do
    [ -e "$p" ] || continue
    local name; name="$(basename "$p" .pid)"; local pid; pid="$(cat "$p")"
    if kill -0 "$pid" 2>/dev/null; then kill "$pid" 2>/dev/null || true; ok "stopped $name (pid $pid)"; fi
    rm -f "$p"
  done
}

urls() {
  cat <<EOF

${B}Stack is up.${X}
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
  need docker; need go
  docker info >/dev/null 2>&1 || die "Docker is not running — start Docker Desktop / the daemon first."
  load_env
  infra_up
  migrate
  services_up
  web_up
  urls
}

cmd_down() {
  stop_procs
  say "Stopping infra…"
  "${COMPOSE[@]}" down && ok "infra stopped (data volumes kept — 'docker compose -f $COMPOSE_FILE down -v' to wipe)"
}

cmd_status() {
  say "Host processes:"
  for s in "${SERVICES[@]}" web; do
    if running "$s"; then ok "$s (pid $(cat "$PID_DIR/$s.pid"))"; else printf '  %s·%s %s stopped\n' "$D" "$X" "$s"; fi
  done
  say "Infra:"; "${COMPOSE[@]}" ps
}

cmd_logs() {
  local s="${1:-core-api}"; local f="$LOG_DIR/$s.log"
  [ -f "$f" ] || die "no log for '$s' (choose: ${SERVICES[*]} web)"
  tail -f "$f"
}

case "${1:-up}" in
  up)      cmd_up ;;
  down)    cmd_down ;;
  restart) cmd_down; cmd_up ;;
  status)  cmd_status ;;
  logs)    cmd_logs "${2:-}" ;;
  *)       die "usage: ./start.sh [up|down|restart|status|logs <service>]" ;;
esac
