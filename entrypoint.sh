#!/bin/sh
set -e

DATA_DIR="${DATA_DIR:-/app/var/nginxpulse_data}"
PGDATA="${PGDATA:-/app/var/pgdata}"
CONFIG_DIR="${CONFIG_DIR:-/app/configs}"
TMPDIR="${TMPDIR:-${DATA_DIR}/tmp}"
POSTGRES_USER="${POSTGRES_USER:-nginxpulse}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-nginxpulse}"
POSTGRES_DB="${POSTGRES_DB:-nginxpulse}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
POSTGRES_LISTEN="${POSTGRES_LISTEN:-127.0.0.1}"
POSTGRES_CONNECT_HOST="${POSTGRES_CONNECT_HOST:-127.0.0.1}"

APP_UID="${PUID:-}"
APP_GID="${PGID:-}"
APP_USER="nginxpulse"
APP_GROUP="nginxpulse"
USE_EMBEDDED_PG=1
if [ -n "${DB_DSN:-}" ]; then
  USE_EMBEDDED_PG=0
fi

if [ -n "$APP_GID" ]; then
  EXISTING_GROUP="$(awk -F: -v gid="$APP_GID" '$3==gid{print $1; exit}' /etc/group)"
  if [ -z "$EXISTING_GROUP" ]; then
    addgroup -S -g "$APP_GID" appgroup
    APP_GROUP="appgroup"
  else
    APP_GROUP="$EXISTING_GROUP"
  fi
fi

if [ -n "$APP_UID" ]; then
  EXISTING_USER="$(awk -F: -v uid="$APP_UID" '$3==uid{print $1; exit}' /etc/passwd)"
  if [ -z "$EXISTING_USER" ]; then
    adduser -S -D -H -u "$APP_UID" -G "$APP_GROUP" appuser
    APP_USER="appuser"
  else
    APP_USER="$EXISTING_USER"
  fi
fi

export TMPDIR
mkdir -p "$DATA_DIR" "$PGDATA" "$TMPDIR" "$CONFIG_DIR"

normalize_web_base_path() {
  local value
  value="$(printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//; s#^/*##; s#/*$##')"
  if [ -z "$value" ]; then
    printf ''
    return 0
  fi
  if printf '%s' "$value" | grep -q '/'; then
    printf ''
    return 0
  fi
  if ! printf '%s' "$value" | grep -Eq '^[A-Za-z0-9_-]+$'; then
    printf ''
    return 0
  fi
  case "$(printf '%s' "$value" | tr 'A-Z' 'a-z')" in
    api|m|assets|favicon.svg|brand-mark|brand-mark.svg|app-config.js|health)
      printf ''
      return 0
      ;;
  esac
  printf '%s' "$value"
}

extract_web_base_path() {
  local raw=""
  if [ -n "${WEB_BASE_PATH:-}" ]; then
    raw="$WEB_BASE_PATH"
  elif [ -n "${CONFIG_JSON:-}" ]; then
    raw="$(printf '%s' "$CONFIG_JSON" | tr '\n' ' ' | sed -n 's/.*\"webBasePath\"[[:space:]]*:[[:space:]]*\"\\([^\\"]*\\)\".*/\\1/p' | head -n 1)"
  elif [ -f "$CONFIG_DIR/nginxpulse_config.json" ]; then
    raw="$(sed -n 's/.*\"webBasePath\"[[:space:]]*:[[:space:]]*\"\\([^\\"]*\\)\".*/\\1/p' "$CONFIG_DIR/nginxpulse_config.json" | head -n 1)"
  fi
  normalize_web_base_path "$raw"
}

write_app_config() {
  local base_path="$1"
  local prefix=""
  if [ -n "$base_path" ]; then
    prefix="/$base_path"
  fi
  printf 'window.__NGINXPULSE_BASE_PATH__ = "%s";\n' "$prefix" > /usr/share/nginx/html/app-config.js
}

write_nginx_conf() {
  local base_path="$1"
  local conf="/etc/nginx/conf.d/default.conf"
  if [ -z "$base_path" ]; then
    cat > "$conf" <<'EOF'
server {
  listen 8088;
  server_name _;
  absolute_redirect off;
  port_in_redirect off;

  root /usr/share/nginx/html;
  index index.html;

  location = /app-config.js {
    add_header Cache-Control "no-store";
    try_files $uri =404;
  }

  location /api/ {
    proxy_pass http://127.0.0.1:8089;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
  }

  location = /m {
    return 302 /m/;
  }

  location /m/ {
    try_files $uri $uri/ /m/index.html;
  }

  location / {
    try_files $uri $uri/ /index.html;
  }
}
EOF
    return 0
  fi

  cat > "$conf" <<EOF
server {
  listen 8088;
  server_name _;
  absolute_redirect off;
  port_in_redirect off;

  root /usr/share/nginx/html;
  index index.html;

  location = /app-config.js {
    add_header Cache-Control "no-store";
    try_files \$uri =404;
  }

  location = /favicon.svg {
    try_files \$uri =404;
  }

  location = /brand-mark.svg {
    try_files \$uri =404;
  }

  location /assets/ {
    try_files \$uri =404;
  }

  location /m/assets/ {
    try_files \$uri =404;
  }

  location = /$base_path {
    return 302 /$base_path/;
  }

  location = /$base_path/m {
    return 302 /$base_path/m/;
  }

  location /$base_path/api/ {
    proxy_pass http://127.0.0.1:8089;
    proxy_http_version 1.1;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
  }

  location /$base_path/m/ {
    rewrite ^/$base_path/m/(.*)$ /m/\$1 break;
    try_files \$uri \$uri/ /m/index.html;
  }

  location /$base_path/ {
    rewrite ^/$base_path/(.*)$ /\$1 break;
    try_files \$uri \$uri/ /index.html;
  }

  location / {
    return 404;
  }
}
EOF
}

is_mount_point() {
  awk -v target="$1" '$2==target {found=1} END {exit found?0:1}' /proc/mounts
}

if ! is_mount_point "$DATA_DIR"; then
  echo "nginxpulse: $DATA_DIR is not a mounted volume. Please bind-mount a host directory to $DATA_DIR." >&2
  exit 1
fi
if [ "$USE_EMBEDDED_PG" = "1" ]; then
  if ! is_mount_point "$PGDATA"; then
    echo "nginxpulse: $PGDATA is not a mounted volume. Please bind-mount a host directory to $PGDATA." >&2
    exit 1
  fi
fi

if [ "$(id -u)" = "0" ]; then
  if ! su-exec "$APP_USER:$APP_GROUP" sh -lc "touch '$DATA_DIR/.write_test' && rm -f '$DATA_DIR/.write_test'" >/dev/null 2>&1; then
    chown -R "$APP_USER:$APP_GROUP" "$DATA_DIR" 2>/dev/null || true
  fi
fi

if ! su-exec "$APP_USER:$APP_GROUP" sh -lc "touch '$DATA_DIR/.write_test' && rm -f '$DATA_DIR/.write_test'" >/dev/null 2>&1; then
  echo "nginxpulse: $DATA_DIR is not writable; file logging may fail and will fall back to stdout" >&2
fi

# Ensure CONFIG_DIR is writable for saving configuration
if [ "$(id -u)" = "0" ]; then
  if ! su-exec "$APP_USER:$APP_GROUP" sh -lc "touch '$CONFIG_DIR/.write_test' && rm -f '$CONFIG_DIR/.write_test'" >/dev/null 2>&1; then
    chown -R "$APP_USER:$APP_GROUP" "$CONFIG_DIR" 2>/dev/null || true
  fi
fi

if ! su-exec "$APP_USER:$APP_GROUP" sh -lc "touch '$CONFIG_DIR/.write_test' && rm -f '$CONFIG_DIR/.write_test'" >/dev/null 2>&1; then
  echo "nginxpulse: $CONFIG_DIR is not writable; configuration saving may fail" >&2
fi

if [ "$USE_EMBEDDED_PG" = "1" ]; then
  if [ "$(id -u)" = "0" ]; then
    if ! su-exec "$APP_USER:$APP_GROUP" sh -lc "touch '$PGDATA/.write_test' && rm -f '$PGDATA/.write_test'" >/dev/null 2>&1; then
      chown -R "$APP_USER:$APP_GROUP" "$PGDATA" 2>/dev/null || true
    fi
  fi

  if ! su-exec "$APP_USER:$APP_GROUP" sh -lc "touch '$PGDATA/.write_test' && rm -f '$PGDATA/.write_test'" >/dev/null 2>&1; then
    echo "nginxpulse: $PGDATA is not writable; postgres may fail to start" >&2
  fi
fi

init_postgres() {
  if [ -s "$PGDATA/PG_VERSION" ]; then
    return 0
  fi

  echo "nginxpulse: initializing postgres data dir at $PGDATA"
  PWFILE="$(mktemp -p "$TMPDIR")"
  # Ensure the postgres user can read the password file created by root.
  chown "$APP_USER:$APP_GROUP" "$PWFILE" 2>/dev/null || true
  chmod 600 "$PWFILE" 2>/dev/null || true
  printf '%s' "$POSTGRES_PASSWORD" > "$PWFILE"
  su-exec "$APP_USER:$APP_GROUP" initdb -D "$PGDATA" \
    --username="$POSTGRES_USER" \
    --pwfile="$PWFILE" \
    --auth-host=md5 \
    --auth-local=trust >/dev/null
  rm -f "$PWFILE"
}

start_postgres() {
  if [ "$(id -u)" = "0" ]; then
    mkdir -p /run/postgresql
    chown "$APP_USER:$APP_GROUP" /run/postgresql 2>/dev/null || true
    chmod 775 /run/postgresql 2>/dev/null || true
  fi
  su-exec "$APP_USER:$APP_GROUP" postgres -D "$PGDATA" \
    -p "$POSTGRES_PORT" \
    -c listen_addresses="$POSTGRES_LISTEN" &
  pg_pid=$!

  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if su-exec "$APP_USER:$APP_GROUP" pg_isready -h "$POSTGRES_CONNECT_HOST" -p "$POSTGRES_PORT" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  return 1
}

ensure_database() {
  export PGPASSWORD="$POSTGRES_PASSWORD"
  if ! su-exec "$APP_USER:$APP_GROUP" psql -h "$POSTGRES_CONNECT_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d postgres -tAc \
    "SELECT 1 FROM pg_database WHERE datname='${POSTGRES_DB}'" | grep -q 1; then
    su-exec "$APP_USER:$APP_GROUP" createdb -h "$POSTGRES_CONNECT_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" "$POSTGRES_DB"
  fi
}

if [ -z "${DB_DSN:-}" ]; then
  export DB_DRIVER="postgres"
  export DB_DSN="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_CONNECT_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"
fi

if [ "$USE_EMBEDDED_PG" = "1" ]; then
  init_postgres
  if ! start_postgres; then
    echo "nginxpulse: postgres did not become ready" >&2
    exit 1
  fi
  ensure_database
fi

if command -v nginx >/dev/null 2>&1; then
  WEB_BASE_PATH_VALUE="$(extract_web_base_path)"
  write_app_config "$WEB_BASE_PATH_VALUE"
  write_nginx_conf "$WEB_BASE_PATH_VALUE"

  su-exec "$APP_USER:$APP_GROUP" /app/nginxpulse "$@" &
  backend_pid=$!
  nginx -g 'daemon off;' &
  nginx_pid=$!

  shutdown() {
    if [ -n "${pg_pid:-}" ]; then
      kill -TERM "$pg_pid" >/dev/null 2>&1 || true
    fi
    if [ -n "${backend_pid:-}" ]; then
      kill -TERM "$backend_pid" >/dev/null 2>&1 || true
    fi
    if [ -n "${nginx_pid:-}" ]; then
      kill -TERM "$nginx_pid" >/dev/null 2>&1 || true
    fi
  }

  trap shutdown INT TERM

  while :; do
    if [ -n "${pg_pid:-}" ] && ! kill -0 "$pg_pid" >/dev/null 2>&1; then
      shutdown
      wait "$pg_pid" >/dev/null 2>&1 || true
      exit 1
    fi
    if [ -n "${backend_pid:-}" ] && ! kill -0 "$backend_pid" >/dev/null 2>&1; then
      shutdown
      wait "$backend_pid" >/dev/null 2>&1 || true
      exit 1
    fi
    if [ -n "${nginx_pid:-}" ] && ! kill -0 "$nginx_pid" >/dev/null 2>&1; then
      shutdown
      wait "$nginx_pid" >/dev/null 2>&1 || true
      exit 1
    fi
    sleep 1
  done
fi

exec su-exec "$APP_USER:$APP_GROUP" /app/nginxpulse "$@"
