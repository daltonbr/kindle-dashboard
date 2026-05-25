# Server

Go HTTP server. Renders dashboard images on demand.

## Responsibilities

1. Expose `GET /dashboard.png` returning a 600×800 grayscale PNG.
2. Fetch upstream data (v1: Open-Meteo weather), with a sane in-memory cache so we don't hammer the API.
3. Compose the dashboard image: panels (v1 has one — weather), labels, fonts.
4. Be trivially deployable to the Docker VM.

## Non-responsibilities (for now)

- Auth, TLS, rate limiting. LAN-only.
- Persistence. Everything is in-memory; restart loses cache, which is fine.
- Pushing to the client. The Kindle pulls.

## Planned layout (not yet created)

```
server/
  main.go                # wiring + http server
  internal/
    render/              # image composition (image, image/draw, image/png)
      dashboard.go       # top-level "render the whole dashboard" function
      panels/
        weather.go       # weather panel renderer
      fonts/             # embedded fonts (TTF) via //go:embed
    weather/
      openmeteo.go       # client + types
      cache.go           # TTL cache for upstream responses
  Dockerfile             # multi-stage; FROM scratch final
  docker-compose.yml     # for the Docker VM deployment
  go.mod
  go.sum
```

## Dependency policy

- **Stdlib first.** `net/http`, `encoding/json`, `image`, `image/draw`, `image/png`, `time`, `context`, `log/slog` should cover the MVP entirely.
- **One acceptable extra: a font rasterizer.** Go's stdlib doesn't include one. Likely candidate: `golang.org/x/image/font` + `golang.org/x/image/font/opentype` — both `golang.org/x` modules, maintained by the Go team, low supply-chain risk.
- **Nothing else without a discussion** and a corresponding entry in [decisions.md](decisions.md).

## Config (env vars)

| Var | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | Listening port inside the container. External mapping handled by compose. |
| `WEATHER_LAT` | TBD | Latitude for Open-Meteo. |
| `WEATHER_LON` | TBD | Longitude for Open-Meteo. |
| `WEATHER_TTL` | `10m` | Min interval between Open-Meteo fetches. |
| `LOG_LEVEL` | `info` | slog level. |

## Endpoints (planned)

| Method | Path | Returns |
| --- | --- | --- |
| GET | `/dashboard.png` | Current dashboard, 600×800 PNG |
| GET | `/healthz` | `200 OK` for container healthcheck |
| GET | `/debug/weather` | JSON of last cached weather payload (dev only) |

Post-MVP: a small config API the Kindle can poll for refresh hints.

## Image rendering notes

- Pre-allocate `image.NewGray()` (4-bit isn't a stdlib pixel format — we'll quantize at encode time to keep the file small, or use `image.Gray16` and let PNG encoding handle it; benchmark when we get there).
- Use embedded TTF (e.g. an open license font) so the binary stays a single artifact.
- Layout in pixels, not points — we're aiming at a known panel.
- Keep the rendered surface in portrait (600 wide × 800 tall). Add rotation later only if we wall-mount it landscape.
