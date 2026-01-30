#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEV_CONFIG="$ROOT_DIR/configs/nginxpulse_config.dev.json"
VERSION="${VERSION:-$(git -C "$ROOT_DIR" describe --tags --abbrev=0 2>/dev/null || echo "dev")}"
BUILD_TIME="${BUILD_TIME:-$(date "+%Y-%m-%d %H:%M:%S")}"
GIT_COMMIT="${GIT_COMMIT:-$(git -C "$ROOT_DIR" rev-parse --short=7 HEAD 2>/dev/null || echo "unknown")}"
LDFLAGS="-s -w -X 'github.com/likaia/nginxpulse/internal/version.Version=${VERSION}' -X 'github.com/likaia/nginxpulse/internal/version.BuildTime=${BUILD_TIME}' -X 'github.com/likaia/nginxpulse/internal/version.GitCommit=${GIT_COMMIT}'"

backend_pid=""
frontend_pid=""
mobile_frontend_pid=""
pg_started_by_us=0

PG_CONTAINER="${PG_CONTAINER:-nginxpulse-postgres}"
PG_VOLUME="${PG_VOLUME:-nginxpulse_pgdata}"
PG_HOST="${PG_HOST:-127.0.0.1}"
PG_PORT="${PG_PORT:-5432}"
PG_USER="${PG_USER:-nginxpulse}"
PG_PASSWORD="${PG_PASSWORD:-nginxpulse}"
PG_DB="${PG_DB:-nginxpulse}"
USE_DOCKER_PG="${USE_DOCKER_PG:-auto}"
DB_DSN="${DB_DSN:-}"
DEFAULT_DSN="postgres://${PG_USER}:${PG_PASSWORD}@${PG_HOST}:${PG_PORT}/${PG_DB}?sslmode=disable"
FORCE_SETUP_UI="${FORCE_SETUP_UI:-}"
FORCE_EMPTY_CONFIG="${FORCE_EMPTY_CONFIG:-}"

ensure_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "$cmd not found in PATH. Please install it and retry." >&2
    exit 1
  fi
}

ensure_go_deps() {
  if [[ ! -f "$ROOT_DIR/go.sum" ]]; then
    echo "go.sum missing, running go mod tidy..."
    (cd "$ROOT_DIR" && GOFLAGS="-mod=mod" go mod tidy)
  fi
}

ensure_config() {
  local config_path="$DEV_CONFIG"
  if [[ ! -f "$config_path" ]]; then
    local base_config="$ROOT_DIR/configs/nginxpulse_config.json"
    if [[ ! -f "$base_config" ]]; then
      echo "configs/nginxpulse_config.json not found. Please create it first." >&2
      exit 1
    fi
    cp "$base_config" "$config_path"
    echo "Created configs/nginxpulse_config.dev.json from nginxpulse_config.json"
    echo "Edit configs/nginxpulse_config.dev.json and re-run." >&2
    exit 1
  fi
}

ensure_frontend_deps() {
  ensure_node_deps "$ROOT_DIR/webapp" "frontend"
}

ensure_mobile_frontend_deps() {
  ensure_node_deps "$ROOT_DIR/webapp_mobile" "mobile frontend"
}

ensure_node_deps() {
  local dir="$1"
  local label="$2"
  local install_needed=0
  if [[ ! -d "$dir/node_modules" ]]; then
    install_needed=1
  elif [[ "$dir/package.json" -nt "$dir/node_modules" ]]; then
    install_needed=1
  elif [[ -f "$dir/package-lock.json" && "$dir/package-lock.json" -nt "$dir/node_modules" ]]; then
    install_needed=1
  fi

  if [[ "$install_needed" -eq 1 ]]; then
    echo "Installing ${label} dependencies..."
    if [[ -f "$dir/package-lock.json" ]]; then
      (cd "$dir" && npm ci) || (cd "$dir" && npm install)
    else
      (cd "$dir" && npm install)
    fi
  fi
}

is_truthy() {
  case "$1" in
    1|true|TRUE|yes|YES|on|ON)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

should_use_docker_pg() {
  if [[ -n "$DB_DSN" ]]; then
    return 1
  fi
  if [[ -z "$USE_DOCKER_PG" || "$USE_DOCKER_PG" == "auto" ]]; then
    return 0
  fi
  if is_truthy "$USE_DOCKER_PG"; then
    return 0
  fi
  return 1
}

start_postgres() {
  if is_truthy "$FORCE_SETUP_UI" || is_truthy "$FORCE_EMPTY_CONFIG"; then
    echo "FORCE_SETUP_UI/FORCE_EMPTY_CONFIG enabled, skipping PostgreSQL startup."
    return 0
  fi
  if ! should_use_docker_pg; then
    return 0
  fi

  ensure_cmd docker

  if docker ps -a --format '{{.Names}}' | grep -qx "$PG_CONTAINER"; then
    if docker ps --format '{{.Names}}' | grep -qx "$PG_CONTAINER"; then
      echo "PostgreSQL container already running: ${PG_CONTAINER}"
    else
      echo "Starting PostgreSQL container: ${PG_CONTAINER}"
      docker start "$PG_CONTAINER" >/dev/null
      pg_started_by_us=1
    fi
  else
    echo "Creating PostgreSQL container: ${PG_CONTAINER}"
    docker run -d --name "$PG_CONTAINER" \
      -e POSTGRES_USER="$PG_USER" \
      -e POSTGRES_PASSWORD="$PG_PASSWORD" \
      -e POSTGRES_DB="$PG_DB" \
      -p "${PG_PORT}:5432" \
      -v "${PG_VOLUME}:/var/lib/postgresql/data" \
      postgres:16 >/dev/null
    pg_started_by_us=1
  fi

  echo "Waiting for PostgreSQL to be ready..."
  for _ in {1..30}; do
    if docker exec "$PG_CONTAINER" pg_isready -U "$PG_USER" -d "$PG_DB" >/dev/null 2>&1; then
      echo "PostgreSQL is ready at ${PG_HOST}:${PG_PORT} (${PG_DB})."
      DB_DSN="$DEFAULT_DSN"
      export DB_DSN
      return 0
    fi
    sleep 1
  done

  echo "PostgreSQL did not become ready in time." >&2
  exit 1
}

start_backend() {
  echo "Starting backend on http://localhost:8089"
  if is_truthy "$FORCE_SETUP_UI" || is_truthy "$FORCE_EMPTY_CONFIG"; then
    local force_setup_ui="0"
    if is_truthy "$FORCE_SETUP_UI"; then
      force_setup_ui="1"
    fi
    local force_empty_config="0"
    if is_truthy "$FORCE_EMPTY_CONFIG"; then
      force_empty_config="1"
    fi
    (cd "$ROOT_DIR" && FORCE_SETUP_UI="$force_setup_ui" FORCE_EMPTY_CONFIG="$force_empty_config" SERVER_PORT=":8089" exec go run -ldflags="${LDFLAGS}" ./cmd/nginxpulse/main.go) &
  else
    if [[ -n "$DB_DSN" ]]; then
      echo "Using DB_DSN from environment or local docker."
    fi
    (cd "$ROOT_DIR" && CONFIG_JSON="$(cat "$DEV_CONFIG")" SERVER_PORT=":8089" DB_DSN="$DB_DSN" exec go run -ldflags="${LDFLAGS}" ./cmd/nginxpulse/main.go) &
  fi
  backend_pid=$!
  sleep 1
  if ! kill -0 "$backend_pid" >/dev/null 2>&1; then
    echo "Backend failed to start. Check if :8089 is already in use." >&2
    exit 1
  fi
}

start_frontend() {
  echo "Starting frontend on http://localhost:8088"
  (cd "$ROOT_DIR/webapp" && npm run dev) &
  frontend_pid=$!
}

start_mobile_frontend() {
  echo "Starting mobile frontend on http://localhost:8087 (LAN: http://<your-ip>:8087)"
  (cd "$ROOT_DIR/webapp_mobile" && npm run dev -- --host 0.0.0.0 --port 8087) &
  mobile_frontend_pid=$!
}

cleanup() {
  if [[ -n "$frontend_pid" ]]; then
    kill "$frontend_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$mobile_frontend_pid" ]]; then
    kill "$mobile_frontend_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$backend_pid" ]]; then
    if command -v pkill >/dev/null 2>&1; then
      pkill -TERM -P "$backend_pid" >/dev/null 2>&1 || true
    fi
    kill "$backend_pid" >/dev/null 2>&1 || true
  fi
  if [[ "$pg_started_by_us" -eq 1 ]]; then
    docker stop "$PG_CONTAINER" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT INT TERM

ensure_cmd go
ensure_cmd node
ensure_cmd npm
ensure_go_deps
if ! is_truthy "$FORCE_SETUP_UI" && ! is_truthy "$FORCE_EMPTY_CONFIG"; then
  ensure_config
else
  echo "FORCE_SETUP_UI enabled, skipping config file checks."
fi
ensure_frontend_deps
ensure_mobile_frontend_deps

start_postgres
start_backend
start_frontend
start_mobile_frontend

wait
