# Server

Go HTTP server. Renders dashboard images on demand.

## Responsibilities

1. Expose `GET /dashboard.png` returning a 600×800 grayscale PNG.
2. Expose `GET /healthz` for container healthchecks.
3. (M3+) Fetch upstream data (weather, etc.) with a sane in-memory cache.
4. (M3+) Compose the dashboard image: panels, labels, fonts.

## Non-responsibilities

- Auth, TLS, rate limiting. LAN-only.
- Persistence. Everything is in-memory; restart loses cache, which is fine.
- Pushing to the client. The Kindle pulls.
- Caching the rendered PNG. We re-render each request (~8 ms); the cache is on **upstream data**, not output. See [D7](decisions.md).

## Layout (current)

```
server/
  go.mod / go.sum
  main.go                       # http + env config + graceful shutdown
  internal/
    render/
      dashboard.go              # Dashboard(w, h, now) -> *image.Gray
      dashboard_test.go         # smoke test: bounds + PNG encoding
  Dockerfile                    # multi-stage, FROM scratch
  .dockerignore
```

M3 will add `internal/weather/` (Open-Meteo client + TTL cache) and embedded TTF assets under `internal/render/fonts/`.

## Dependency policy

- Stdlib first. `net/http`, `image`, `image/draw`, `image/png`, `time`, `context`, `log/slog`, `os/signal` cover M2 entirely.
- One outside dep: `golang.org/x/image/font/basicfont` — Go-team-maintained bitmap font for placeholder text. See [D8](decisions.md).
- Nothing else without a discussion + a corresponding entry in [decisions.md](decisions.md).

## Configuration (env vars)

| Var | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | Listening port inside the container. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. Standard `slog.Level` text. |

M3 will add `WEATHER_LAT`, `WEATHER_LON`, `WEATHER_TTL`.

## Endpoints

| Method | Path | Returns |
| --- | --- | --- |
| GET | `/dashboard.png` | Current dashboard, 600×800 8-bit grayscale PNG. `Cache-Control: no-store`. |
| GET | `/healthz` | `200 OK` body `ok\n`. |

## Container image

Published to **`ghcr.io/daltonbr/kindle-dashboard`** on every push to `main`. Tags:

| Tag | Meaning |
| --- | --- |
| `latest` | tip of `main` |
| `sha-<short>` | a specific commit, for pinning / rollback |

Image is:

- `FROM scratch` — no shell, no package manager
- Static Go binary, stripped, `-trimpath`'d
- Runs as `USER 65534:65534` (nobody/nogroup)
- Exposes 8080 (overridable via `PORT`)
- ~8 MB on disk, ~2.5 MB content

See [D9](decisions.md) for rationale.

## Local development

```sh
cd server

# Dependency sync, vet, test, lint, build
go mod tidy
go vet ./...
go test -race ./...
go build .

# Run
PORT=8765 ./server

# Smoke test
curl -fsS -o /tmp/dashboard.png http://localhost:8765/dashboard.png
curl -fsS http://localhost:8765/healthz
```

Same checks run in CI; see [`.github/workflows/ci.yml`](../.github/workflows/ci.yml).

To test the docker image locally:

```sh
docker build -t kindle-dashboard-server:dev ./server
docker run --rm -p 8765:8080 kindle-dashboard-server:dev
```

## Image rendering notes

- The renderer returns `*image.Gray` directly (8-bit gray); `image/png` encodes it without conversion.
- `basicfont.Face7x13` is small (~3 mm tall at 167 PPI). Readable but not pretty — placeholder for M3.
- Pre-rendered the panel in **portrait** (600 wide × 800 tall). Adding rotation later only if we wall-mount it landscape.

## Deployment contract

The image is intentionally vanilla so any container orchestrator can run it. The operator wires up port mapping, restart policy, healthcheck, etc.

- Single container, no sidecars, no volumes, no dependencies.
- Listens on `${PORT:=8080}` inside the container.
- Healthcheck: `GET /healthz`.
- Suggested restart policy: `unless-stopped` (it's a wall-mounted backend, restart on host reboot is desirable).
- No secrets needed for M2. M3 will not need them either (Open-Meteo is keyless).

### Deploy recipes

Pick whichever fits the deploy tooling. Both assume the image has been pulled from GHCR (it's public, no login required).

**Plain `docker run`** (smoke test or one-shot):

```sh
docker run -d \
  --name kindle-dashboard \
  --restart unless-stopped \
  -p 8080:8080 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --health-cmd 'wget -qO- http://127.0.0.1:8080/healthz || exit 1' \
  --health-interval 30s \
  --health-timeout 3s \
  --health-retries 3 \
  ghcr.io/daltonbr/kindle-dashboard:latest
```

Notes:
- Change the **host** port (`-p HOST:8080`) if 8080 is taken. Container port stays 8080.
- `--read-only` is safe — the server writes nothing to disk.
- The `wget` healthcheck **will not work against the `FROM scratch` image** as written (no shell, no wget). Either drop `--health-*` and rely on host-side checks, or use a sidecar healthcheck container. Resolved properly in M4.3.

**Compose** (typical):

```yaml
services:
  kindle-dashboard:
    image: ghcr.io/daltonbr/kindle-dashboard:latest
    container_name: kindle-dashboard
    restart: unless-stopped
    ports:
      - "8080:8080"    # change host port if 8080 is taken
    environment:
      PORT: "8080"
      LOG_LEVEL: "info"
    read_only: true
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
    # healthcheck: see note above — FROM scratch has no shell/wget.
    # Add a host-side check or a sidecar; M4.3 will resolve in-binary.
```

To pin a specific build instead of `:latest`:

```
image: ghcr.io/daltonbr/kindle-dashboard:sha-<short>
```

Tags are visible at https://github.com/daltonbr/kindle-dashboard/pkgs/container/kindle-dashboard.

### Post-deploy checklist

From any LAN machine:

```sh
curl -fsS http://<server-host>:<port>/healthz                 # → ok
curl -fsS -o /tmp/dash.png http://<server-host>:<port>/dashboard.png
file /tmp/dash.png                                            # → PNG image data, 600 x 800, 8-bit grayscale
```

Once the server is live, update the Kindle's `config.env` to point at it:

```sh
ssh kindle 'cat > /mnt/us/dashboard/config.env <<EOF
SERVER_URL=http://<server-host>:<port>/dashboard.png
LOG_LINES=500
EOF'
```

…and tear down any dev server.
