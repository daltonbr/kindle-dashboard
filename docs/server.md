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

Once the server is live, update the Kindle's `config.env` to point at it:

```sh
ssh kindle 'cat > /mnt/us/dashboard/config.env <<EOF
SERVER_URL=http://<server-host>:<port>/dashboard.png
LOG_LINES=500
EOF'
```

…and tear down any dev server.
