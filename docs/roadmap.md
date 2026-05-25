# Roadmap

Milestones, roughly in order. Each one ends with a working, demonstrable thing — no half-states.

## M0 — Repo bootstrap ✅

- [x] Local git repo
- [x] Initial docs (`device.md`, `architecture.md`, `decisions.md`, `client.md`, `server.md`, `roadmap.md`)
- [x] README

## M1 — Client display pipeline (no real server yet) ✅

Goal: a static PNG, served from any HTTP source, ends up on the Kindle panel via cron.

- [x] SSH into the Kindle, answer the open questions in [device.md](device.md) — see [recon 2026-05-25](recon/2026-05-25-first-ssh.md)
- [x] Confirm `eips` flags and writable paths
- [x] Write `client/refresh.sh` and install it to `/mnt/us/dashboard/`
- [x] Stand up a cron entry (`* * * * *` for dev) — busybox crond auto-picks up the new entry
- [x] Test with a 600×800 grayscale PNG served from the Mac (`python3 -m http.server 8765`)
- [ ] Verify the cron survives a Kindle reboot (deferred — low risk, easy to test later)

**Definition of done met:** image visibly drawn on the Kindle, refreshed once per minute by cron with `ok` log lines, served from the Mac at `10.0.0.184:8765`.

Open items deferred to post-M2 (don't block progress):
- Suppress reader UI / lock screen overlay during refresh (leading candidate: linkss screensaver pipeline in "last screen" mode, see [architecture.md](architecture.md)).
- Battery / wake-from-sleep behavior under cron-driven refresh. **Observed during M2:** after ~30 min idle the Kindle enters deep sleep — Wi-Fi drops, cron either doesn't fire or fires with no network, ssh unreachable until a button tap. So the dashboard currently only refreshes while the device is "awake". The BatteryStatus extension may help us inspect this; ultimately the linkss "last screen" path will sidestep it entirely.

## M2 — Minimal Go server returning a static dashboard

- [x] `server/main.go` with `GET /dashboard.png` returning a generated 600×800 grayscale PNG (title, timestamp, grayscale ramp). `GET /healthz` for container healthchecks.
- [x] `go.mod` + stdlib + `golang.org/x/image/font/basicfont` (Go-team-maintained, tiny built-in bitmap font; nicer TTF lands in M3)
- [x] `Dockerfile` (multi-stage, `FROM scratch`, non-root `USER 65534`, static stripped binary, ~8MB image)
- [x] Local end-to-end: Mac runs the binary on port 8765, kindle cron pulls and `eips`-renders the Go image (confirmed visually on the panel)
- [x] CI: `.github/workflows/ci.yml` — go vet, golangci-lint, go test -race, go build, shellcheck on `client/*.sh`
- [x] Release: `.github/workflows/release.yml` — builds and pushes `ghcr.io/daltonbr/kindle-dashboard:latest` + `:sha-<short>` on push to main
- [x] Deploy to the operator's Docker host
- [x] Update kindle's `config.env` to point at the deployed server

**Definition of done met. M2 closed.**

## M3 — Weather panel (Open-Meteo)

**Definition of done:** weather information shows on the Kindle and updates within `WEATHER_TTL` of a real-world change. Deployed via GHCR.

Each sub-task below is small enough to land as its own PR; the order matters because later steps depend on earlier ones.

### M3.1 — Open-Meteo client (no UI yet) ✅

- [x] `server/internal/weather/openmeteo.go` — small typed client. One method: `Fetch(ctx, lat, lon) (Forecast, error)`. Uses `net/http` and `encoding/json`; no third-party deps.
- [x] Types pinned to what we actually render: current temp, current conditions code, today's high/low, next 24h hourly temperatures.
- [x] Unit test against a `httptest.Server` returning a canned Open-Meteo response payload (committed `testdata/brighton.json`).

### M3.2 — TTL cache around the client ✅

- [x] `server/internal/weather/cache.go` — thread-safe wrapper. `Get(ctx) (Forecast, error)` returns the cached value if fresh, refetches if stale. Single-flight on refetch (channel-broadcast pattern; 50-goroutine cold-start test asserts exactly one upstream call).
- [ ] Env-driven TTL: `WEATHER_TTL` (default `10m`). *(env wiring happens in M3.6; the Cache constructor takes a `ttl time.Duration`.)*
- [x] Test: assert a second `Get` within TTL doesn't hit upstream. Also: TTL expiry refetches, errors aren't cached, single-flight coalesces concurrent cold-starts, ctx cancellation while waiting on inflight, `ttl=0` disables caching.

### M3.3 — Add CA certs to the Docker image ✅

- [x] Builder installs `ca-certificates`; final scratch image gets `COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt`. Documented in D9 (already mentioned the eventual COPY; no new D needed).
- [x] Verified locally: built the image, ran a static Go probe inside it against `https://api.open-meteo.com/v1/forecast` → `200 OK` + real JSON. No x509 errors.

### M3.6 — Wire it all up in `main.go` ✅ (done before M3.4/M3.5 — see note below)

- [x] Read `WEATHER_LAT`, `WEATHER_LON`, `WEATHER_TTL` from env (defaults: Brighton, 10m).
- [x] Construct the cached client at boot. Inject into the handler.
- [x] On `GET /dashboard.png`, ask the cached client for a forecast (8s per-request timeout; cron-side curl timeout is 20s).
- [x] Handler renders "(weather unavailable)" if `cache.Get` errors. Last-good fallback deferred.

### M3.7 — Deploy + verify on device ✅

- [x] Push to main → GHCR publishes new `:latest`.
- [x] Operator pulled and restarted the container (running with Brighton defaults; env-var plumbing is an operator TODO in `docs/server.md`).
- [x] `/dashboard.png` from the deployed server shows real weather.
- [x] Confirmed on the Kindle panel.

### M3.5 — Compose the weather panel (after first deploy)

Reordered: wired into `main.go` first with the M2-style basicfont layout so we can see weather on the wall sooner. M3.5 then refactors the layout, M3.4 swaps fonts.

- [ ] Refactor `render.Dashboard` to delegate to `panels.Weather(ctx, w *image.Gray, area image.Rectangle, forecast Forecast)`. The "panel" abstraction is what lets M4+ stack more cards.
- [ ] Layout: large current temp + condition word, smaller "today H/L", a 24h temperature curve at the bottom (skip if it's getting complicated; a row of hourly numbers is fine).

### M3.4 — Real fonts (embed a TTF) (after M3.5)

- [ ] Pick an open-license TTF (candidates: Inter, IBM Plex Sans, Atkinson Hyperlegible). Commit it under `server/internal/render/fonts/`.
- [ ] Add `golang.org/x/image/font/opentype` to `go.mod` and document in [decisions.md](decisions.md) (new D entry).
- [ ] Embed with `//go:embed`. Provide a `Face(size float64) font.Face` helper in `internal/render/fonts/`.
- [ ] Migrate existing text to the new font; delete the basicfont references.

---

## M4 — Polish + reliability

Big-picture goals: dashboard stays visible 24/7 with sensible battery life, refreshes survive Wi-Fi blips, and the device is mounted somewhere sensible.

### M4.1 — Sleep / always-on path

- [ ] Investigate `linkss` "last screen" mode (already-installed jailbreak hack). Write the dashboard PNG to `/mnt/us/linkss/screensavers/bg_ss00.png` instead of (or in addition to) calling `eips -g`. Document findings in a new `docs/recon/<date>-linkss.md`.
- [ ] Alternatively: `lipc-set-prop com.lab126.powerd preventScreenSaver 1` to keep the device awake on AC power. Less power-efficient but simpler.
- [ ] Pick one path; record the decision in `docs/decisions.md`.

### M4.2 — Production cron cadence

- [ ] Change `/etc/crontab/root` entry from `* * * * *` to `*/15 * * * *` (same mntroot dance as the install).
- [ ] Reflect the change in `docs/client.md`.

### M4.3 — Healthcheck wiring

- [ ] If the operator's compose includes a `HEALTHCHECK`, ensure `GET /healthz` works from inside the container with no shell (currently fine — Go binary itself is the only thing in the image; HEALTHCHECK needs to use the binary or be docker's `CMD-SHELL` with `wget`/`curl` *inside* — neither exists in `FROM scratch`). Options: add a `/healthz` hint command in the binary itself (`./server healthcheck`), or accept that the host-side healthcheck is the only viable place.
- [ ] Document the choice.

### M4.4 — Cron survival across reboots

- [ ] Reboot the Kindle (long-press power → restart). Verify the cron entry is still in `/etc/crontab/root` after boot and that crond fires it.

### M4.5 — Battery / mount

- [ ] Drive-test the BatteryStatus extension; add a battery line to the dashboard PNG.
- [ ] Decide on wall-mount hardware + power delivery (USB cable run, dock, magsafe-style?).
- [ ] Long-soak test (24h) and capture battery drain.

---

## Post-M4 ideas (not committed)

Pull from this list when M4 is done — don't start in parallel.

- Calendar panel (CalDAV / Google Calendar via a local sync helper)
- Kanban / chore tracker panel
- "Now playing" panel
- Multiple dashboard layouts selected via query param (`?layout=morning`, etc.)
- Configuration API the Kindle polls for refresh hints
- Support for additional eink devices at other resolutions
- Migrate `client/refresh.sh` install to a small `client/install.sh` once we have >1 client artifact
