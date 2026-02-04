# Deployment

## Version requirement
- Version > 1.5.3 requires PostgreSQL. SQLite is deprecated.

## Docker single-container (built-in PostgreSQL)
The image includes PostgreSQL. Recommended for most users.

One-click start (minimal config, first launch opens Setup Wizard):
```bash
docker run -d --name nginxpulse \
  -p 8088:8088 \
  -e PUID=1000 \
  -e PGID=1000 \
  -v ./docker_local/logs:/share/logs:ro \
  -v ./docker_local/nginxpulse_data:/app/var/nginxpulse_data \
  -v ./docker_local/pgdata:/app/var/pgdata \
  -v /etc/localtime:/etc/localtime:ro \
  magiccoders/nginxpulse:latest
```

Example:
```bash
docker run -d --name nginxpulse \
  -p 8088:8088 -p 8089:8089 \
  -e PUID=1000 \
  -e PGID=1000 \
  -e WEBSITES='[{"name":"Main","logPath":"/share/log/nginx/access.log","domains":["example.com"]}]' \
  -v /path/to/nginx/access.log:/share/log/nginx/access.log:ro \
  -v /path/to/nginxpulse_data:/app/var/nginxpulse_data \
  -v /path/to/pgdata:/app/var/pgdata \
  -v /etc/localtime:/etc/localtime:ro \
  nginxpulse:latest
```

Useful env vars (built-in PG):
- `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB`
- `POSTGRES_PORT` (default 5432)
- `POSTGRES_LISTEN` (default 127.0.0.1)
- `POSTGRES_CONNECT_HOST` (default 127.0.0.1)
- `DATA_DIR` (default `/app/var/nginxpulse_data`)
- `PGDATA` (default `/app/var/pgdata`)

If you want to use an external PG, set `DB_DSN` and the built-in PG will be bypassed.
In this case the built-in PG will not start, `POSTGRES_*` is ignored, and you can drop the `/app/var/pgdata` mount.

## Docker Compose
A `docker-compose.yml` is provided in the repo. Update:
- `WEBSITES` and log volume
- Configure `PUID/PGID` to align with host UID/GID if you hit permission issues.
- `nginxpulse_data` volume; mount `pgdata` only when using built-in PG
- `/etc/localtime` mount for timezone
- On SELinux hosts (RHEL/CentOS/Fedora), append `:z` or `:Z` to the volume options.

One-click start (minimal config, first launch opens Setup Wizard):
```bash
docker compose -f docker-compose-simple.yml up -d
```

## Docker Deployment Permissions

The image runs as a non-root user (`nginxpulse`) by default. Whether the app can read logs or write data depends on **host directory permissions**. If you can `cat` files via `docker exec`, you are likely root; it does not mean the app user can access them.

Recommended approach: **align container UID/GID with host directory ownership**.

Step 1: Check host directory UID/GID
```bash
ls -n /path/to/logs /path/to/nginxpulse_data /path/to/pgdata
# or
stat -c '%u %g %n' /path/to/logs /path/to/nginxpulse_data /path/to/pgdata
```

Step 2: Pass `PUID/PGID` when starting the container
```bash
docker run ... \
  -e PUID=1000 \
  -e PGID=1000 \
  -v /path/to/logs:/var/log/nginx:ro \
  -v /path/to/nginxpulse_data:/app/var/nginxpulse_data:rw \
  -v /path/to/pgdata:/app/var/pgdata:rw \
  ...
```

Step 3: Ensure directories are readable/writable for that UID/GID
```bash
chown -R 1000:1000 /path/to/nginxpulse_data /path/to/pgdata
chmod -R u+rx /path/to/logs
```

If you use an external database (`DB_DSN`), you can skip mounting `pgdata`.

SELinux note (RHEL/CentOS/Fedora):
- These systems enable SELinux by default. Docker volumes may be visible but still inaccessible due to labels.
- Add `:z` or `:Z` to re-label the mount:
  - `:Z` for exclusive use by this container.
  - `:z` to share across multiple containers.
```bash
docker run ... \
  -v /path/to/logs:/var/log/nginx:ro,Z \
  -v /path/to/nginxpulse_data:/app/var/nginxpulse_data:rw,Z \
  -v /path/to/pgdata:/app/var/pgdata:rw,Z \
  ...
```

Not recommended: `chmod -R 777`. It is unsafe; only use it for temporary debugging.

## Single binary (non-Docker)
You must install PostgreSQL yourself.

Suggested steps:
1. Install PostgreSQL and create DB/user.
2. Set `database.dsn` in `configs/nginxpulse_config.json`.
3. Build & run (e.g. `scripts/build_single.sh`).

Example DSN:
```json
"database": {
  "driver": "postgres",
  "dsn": "postgres://nginxpulse:nginxpulse@127.0.0.1:5432/nginxpulse?sslmode=disable",
  "maxOpenConns": 10,
  "maxIdleConns": 5,
  "connMaxLifetime": "30m"
}
```

## Local development
Use `scripts/dev_local.sh`:
- It starts a local docker postgres container by default.
- Data is stored in docker volume `nginxpulse_pgdata`.
- To reset: `docker volume rm nginxpulse_pgdata`.

## Ports
- 8088: Web UI
- 8089: API

## Custom frontend base path
If you want to serve the UI under a subpath (e.g. `/nginxpulse/`), set `system.webBasePath`. Only a **single path segment** is supported. After changing it, restart the service. The root path `/` will be disabled.

### Docker image (built-in Nginx)
1. Set `webBasePath` in system config (or `WEB_BASE_PATH` env var).
2. Restart the container. New paths:
   - PC: `/<base>/`
   - Mobile: `/<base>/m/`
   - API: `/<base>/api/`

The image generates Nginx config and `/app-config.js` on startup. No frontend rebuild needed.

### Single binary (embedded frontend)
1. Set `system.webBasePath` in the config file.
2. Restart. New paths:
   - PC: `/<base>/`
   - Mobile: `/<base>/m/`
   - API: `/<base>/api/`

### Design principles
1. `/app-config.js` delivers `window.__NGINXPULSE_BASE_PATH__` so the frontend resolves base path at runtime.
2. Server middleware enforces prefix isolation; only `/<base>/...` is allowed and API is forced to `/<base>/api/*`.
3. Static assets and `/app-config.js` stay accessible so the app can boot.

## Timezone
The project uses system timezone for parsing.
- Docker: mount `/etc/localtime:/etc/localtime:ro`
- Bare metal: set system timezone and restart
