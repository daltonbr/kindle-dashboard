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
| `DASHBOARD_ORIENTATION` | `portrait` | Default page orientation: `portrait` (600×800) or `landscape` (800×600). Server-side so the device fetches a bare `/dashboard.png`; `?orientation=` overrides per-request (used by `/preview`). |
| `DASHBOARD_TIMEZONE` | `Europe/London` | IANA zone used to render all clock values (agenda event times, header date, "updated HH:MM", month grid). Must be the wall's real local zone — the `FROM scratch` image has `time.Local == UTC`, so without this every time renders in GMT (an hour behind during BST). An IANA name (not a fixed offset) means BST↔GMT is handled automatically. Default matches the Brighton weather coordinates. |
| `WEATHER_PROVIDER` | `openmeteo` | `openmeteo` (live Open-Meteo client+cache, the default) or `demo` (network-free fixture, for widget development and offline runs). |
| `WEATHER_LAT` | `50.8225` | Latitude for the Open-Meteo lookup. Default is Brighton, UK. *(only used when `WEATHER_PROVIDER=openmeteo`)* |
| `WEATHER_LON` | `-0.1372` | Longitude for the Open-Meteo lookup. *(openmeteo only)* |
| `WEATHER_TTL` | `10m` | Any `time.Duration`. The upstream cache TTL; `0` disables caching. *(openmeteo only)* |
| `CALENDAR_ICS_URL` | _(unset)_ | **Secret.** A read-only iCal feed URL (e.g. a Google Calendar "Secret address in iCal format") for the agenda card. **Unset ⇒ no agenda card** (inert/clone-safe). Treat as a credential — env/vault only, never committed. See [D19](decisions.md#d19--calendar-auth-google-calendar-secret-ical-url) + [Secret hygiene](#secret-hygiene--config-public-repo). |
| `CALENDAR_PROVIDER` | _(unset)_ | Set to `demo` to render the agenda from a network-free fixture (widget development / offline). When `demo`, `CALENDAR_ICS_URL` is ignored. |
| `CALENDAR_TTL` | `15m` | Any `time.Duration`. Cache TTL for the iCal feed. *(only used when `CALENDAR_ICS_URL` is set)* |

> **M5 note:** the server defaults to `WEATHER_PROVIDER=openmeteo`. The
> Open-Meteo client fetches hourly precipitation (probability + amount) and a
> 3-day daily outlook (hi/lo, weather code, peak precip probability), so the rain
> card and 3-day forecast render live data (roadmap M5.3). Set
> `WEATHER_PROVIDER=demo` to develop the widget layout against the network-free
> fixture.

> **Operator TODO:** expose `WEATHER_LAT` / `WEATHER_LON` / `WEATHER_TTL` as variables in the deployment config so they can be tuned without rebuilding the image. Currently relying on the image's built-in Brighton defaults.

> **M6 note (calendar):** the agenda card (bottom-left cell, shown when rain is in
> the footer) is driven by `CALENDAR_ICS_URL`. It is **inert when unset** — the
> public image and any clone show no agenda until the operator supplies the feed
> URL at run time. The URL is a **secret** (a bearer credential): inject it from
> the host vault via `--env-file`, never bake it into the image or commit it.
> Example `--env-file` line (placeholder — **not** a real URL):
> ```
> CALENDAR_ICS_URL=https://calendar.google.com/calendar/ical/REDACTED/private-REDACTED/basic.ics
> ```
> For layout work without a real feed, set `CALENDAR_PROVIDER=demo`. The image
> stays `FROM scratch`; timezone resolution for `TZID` events uses the embedded
> Go `time/tzdata` (no system zoneinfo needed). See [D20](decisions.md#d20--ics-parsing-stdlib-bounded-horizon-recurrence-no-ical-dependency).

### Secret hygiene & config (public repo)

The repo is public. The rule (see [D16](decisions.md#d16--widget-data-layer-in-repo-providers-secrets-via-env)): **code is public; secrets and personal identifiers are not.** From M5 onward, providers may need API keys/tokens (e.g. OpenWeatherMap, Home Assistant, Google Calendar) and personal IDs (calendar IDs, HA entity IDs/URL, exact coords).

- **Secrets and personal config come from the environment only** — env vars or a host-only mounted file. Never committed; never baked into the image (it stays `FROM scratch`, binary only — secrets enter at `docker run` time via `--env-file`).
- **This doc lists vars with placeholder values only.** Never paste a real key/token/ID here.
- **Providers are inert without config** — a provider whose env vars are unset returns an error (the widget renders "unavailable") or falls back to a `Demo*` source. A clone of the repo with no config does nothing private, and CI/forks run clean.
- **Guards:** GitHub secret scanning + push protection (free on public repos) are on in repo settings, and a `gitleaks` job runs in CI on every push/PR ([D18](decisions.md#d18--secret-scan-ci-guard-gitleaks)). To catch a leak *before* it commits, scan locally first:
  ```sh
  # one-off, before pushing — scans the working tree + history
  gitleaks git --no-banner .
  # or wire it as a pre-commit hook (scans only staged changes)
  gitleaks protect --staged --no-banner
  ```
- Privacy is **not** the security mechanism — these practices apply whether or not the repo is public.

## Endpoints

| Method | Path | Returns |
| --- | --- | --- |
| GET | `/dashboard.png` | Current dashboard, 600×800 8-bit grayscale PNG. `Cache-Control: no-store`. |
| GET | `/healthz` | `200 OK` body `ok\n`. |
| GET | `/preview` | HTML preview page wrapping `/dashboard.png`. |

**`/dashboard.png` query params** (all optional). Layout decisions live
server-side (orientation, rain placement), so the wall device only ever sends
its own telemetry — `batt`/`plug`. The rest exist mainly for `/preview`:

| Param | Values | Effect |
| --- | --- | --- |
| `orientation` | `portrait`/`landscape` | Override the server default (`DASHBOARD_ORIENTATION`) for this request. |
| `rain` | `card` | Render the rain timeline as the in-grid 2×1 card. **Default is the footer strip** — the placement decision lives server-side so the device fetches a bare `/dashboard.png`. |
| `batt` | `0`–`100` | Show the battery indicator at that level (absent ⇒ no indicator). |
| `plug` | `1`/`true` | With `batt`, overlays a charging bolt. |

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
- No secrets needed through M4 (Open-Meteo is keyless). M5 providers may need keys/tokens — injected via env / a host-only `--env-file`, never in the image. See [Secret hygiene & config](#secret-hygiene--config-public-repo).

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
  ghcr.io/daltonbr/kindle-dashboard:latest
```

Notes:
- Change the **host** port (`-p HOST:8080`) if 8080 is taken. Container port stays 8080.
- `--read-only` is safe — the server writes nothing to disk.
- **Healthcheck is baked into the image** (M4.4): the Dockerfile's `HEALTHCHECK` runs `/server healthcheck`, which the binary answers itself (no shell/wget needed in `FROM scratch`). It probes `/healthz` on `127.0.0.1:$PORT`. Override with `--health-cmd '/server healthcheck'` only if you need different interval/timeout; otherwise nothing to add.

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
    # The image ships a HEALTHCHECK (/server healthcheck). Override here only to
    # tune timing or the port:
    # healthcheck:
    #   test: ["CMD", "/server", "healthcheck"]
    #   interval: 30s
    #   timeout: 3s
    #   retries: 3
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
