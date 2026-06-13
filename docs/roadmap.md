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

### M3.5 — Compose the weather panel (after first deploy) ✅

Reordered: wired into `main.go` first with the M2-style basicfont layout so we can see weather on the wall sooner. M3.5 then refactored the layout, M3.4 swaps fonts.

- [x] `server/internal/render/panels/weather.go` with `Weather(dst, area, forecast)`. `dashboard.go` now only owns header/footer/panel placement.
- [x] Layout: current temp, condition word (WMO code → label), today H/L, observation time, fetch time, and a Bresenham line chart of the next 24h with min/max labels and 6-hourly tick marks along the bottom axis.

### M3.4 — Real fonts (embed a TTF) (after M3.5) ✅

- [x] Atkinson Hyperlegible (OFL) committed to `server/internal/render/fonts/` alongside `OFL.txt`.
- [x] `golang.org/x/image/font/opentype` pulled in via existing `golang.org/x/image` dep. New decision: [D13](decisions.md).
- [x] `//go:embed` + `fonts.Face(sizePx float64) font.Face` with per-size caching.
- [x] Migrated `panels/weather.go` and `dashboard.go` to the new font; basicfont references deleted.

---

## M4 — Polish + reliability

Big-picture goals: dashboard stays visible 24/7 with sensible battery life, refreshes survive Wi-Fi blips, and the device is mounted somewhere sensible.

### M4.1 — Sleep + scheduled wake (recon ✅, daemon impl pending)

**Architecture chosen and validated end-to-end on 2026-05-25.** See [D14](decisions.md) and the full investigation in [recon 2026-05-25-wake-investigation](recon/2026-05-25-wake-investigation.md). Implementation lands as a follow-up sub-task below.

Recon outcomes:

- [x] Investigated `linkss` screensaver pipeline. Works as a "stay visible while sleeping" mechanism but cron is suspended along with the kernel — refreshes only happen when someone taps the device. Insufficient on its own. See [recon 2026-05-25-linkss](recon/2026-05-25-linkss.md).
- [x] Tested `preventScreenSaver`-driven always-on. Works empirically, but burns Wi-Fi+CPU baseline 24/7. Rejected: the days-on-battery payoff of an eink panel is the reason we picked this device.
- [x] Validated sleep+wake architecture: `/sys/class/rtc/rtc1/wakealarm` + `echo mem > /sys/power/state` works cleanly. LIPC `rtcWakeup`/`wakeUp` are *declared* but unimplemented on this firmware. Three-cycle integrated test (suspend → wake → Wi-Fi nudge → curl) succeeded 3/3 with ~7s post-wake reassociation.
- [x] Identified prerequisites: `stop framework` + `stop lab126_gui` (eliminates `cvm` JIT crashes that otherwise abort suspends), and a `wirelessEnable 0/1` LIPC nudge after each wake (without the framework, Wi-Fi doesn't reassociate on its own).
- [x] Confirmed `scaling_governor=powersave` (396 MHz) sticks across resume.

### M4.2 — Implement the sleep+wake daemon ✅

Deployed and validated on 2026-05-26. The daemon ran ~10 hours unattended overnight at INTERVAL=300, ~125 successful cycles, no fast-return cascades, no fetch failures after the first post-wake reassociation completed.

- [x] New [`client/loop.sh`](../client/loop.sh) running the prelude + loop documented in [D14](decisions.md) / [recon 2026-05-25-wake-investigation](recon/2026-05-25-wake-investigation.md).
- [x] PID file at `$ROOT/state/loop.pid`. Single-instance guard is pidfile + `/proc` cmdline scan (the scan catches orphan daemons whose pidfile got removed while they were blocked in `echo mem`, which masks signals).
- [x] Replaced per-minute `* * * * *` crontab entry with `@reboot /mnt/us/dashboard/loop.sh`.
- [x] Watchdog: `*/5 * * * * /mnt/us/dashboard/watchdog.sh` — relaunches the daemon if its PID is stale.
- [x] Default `INTERVAL=300` (5 min) for the first soak, exposed via `config.env`.
- [x] Fast-return guard: if a cycle's `echo mem` returns in `< INTERVAL/2`, sleep the remainder before next iter.
- [x] Maintenance mode: `touch state/maintenance` to make the daemon skip suspend and poll every 30s instead, so an operator can ssh in to edit configs without racing the sleeper.
- [x] Overnight soak: 10+ hours at 5-min interval, ~125 successful cycles. Battery sampling into `state/batt.csv` working.

### M4.3 — Interval policy: production cadence + time-of-day awareness

- [x] Bumped `INTERVAL` from soak value (5 min) to 10 min (600s) on 2026-05-26. Daemon (pid 7653) running with the new cadence; first cycle confirmed `cycle … < 600s/2 — sleeping …` in the log.
- [ ] After ~24h of 10-min cycles, review `state/batt.csv` slope and decide whether to push toward 15 min.
- [x] Schedule-aware interval (2026-06-13): `loop.sh` now picks its interval per-cycle from the wall clock — `INTERVAL` (10 min) by day, `NIGHT_INTERVAL` (1 h) from 00:00–07:00. Config-driven via `NIGHT_INTERVAL`/`NIGHT_START`/`NIGHT_END`; defaults bake in the policy. See [D15](decisions.md#d15--time-of-day-refresh-cadence) and [client.md](client.md#refresh-cadence-and-how-to-tweak-it). **Not yet deployed to the device** — pending an ssh window (device currently suspended).

### Open observation window (started 2026-05-26 ~11:11 BST)

Daemon left running unattended at `INTERVAL=600`. When picking this back up, look for:

- **`state/batt.csv` slope** — informs M4.6 (drain rate at the 10-min cadence) and feeds the M4.3 "should we go to 15 min?" decision.
- **`fetch FAILED` clusters in `state/loop.log`** — Wi-Fi flakiness patterns. Isolated failures are fine (the next cycle picks up), repeated clusters at the same time-of-day might hint at router behaviour.
- **Orphan-loop or guard-exit log lines** — `another loop.sh is already running` should NOT appear under steady-state operation. If it does, the watchdog and the daemon are racing for some reason.
- **Ghost-refresh cycles** — every 12 cycles now = every 2h at the new cadence. Should see one of these per ~2h in the log.

### M4.4 — Healthcheck wiring (server-side)

- [ ] If the operator's compose includes a `HEALTHCHECK`, ensure `GET /healthz` works from inside the container with no shell (currently fine — Go binary itself is the only thing in the image; HEALTHCHECK needs to use the binary or be docker's `CMD-SHELL` with `wget`/`curl` *inside* — neither exists in `FROM scratch`). Options: add a `/healthz` hint command in the binary itself (`./server healthcheck`), or accept that the host-side healthcheck is the only viable place.
- [ ] Document the choice.

### M4.5 — Daemon survival across reboots ✅

Tested on 2026-05-26 via `ssh kindle /sbin/reboot`. The daemon **does** survive a reboot, but not via the path we expected:

- **`@reboot /mnt/us/dashboard/loop.sh` is silently ignored** by busybox crond on this firmware. crond comes up at boot (verified — PID 853), reads `/etc/crontab/root`, but never executes the `@reboot` entry. No new "loop.sh starting" log line; no `loop.sh` process in `/proc`.
- **The watchdog cron fills the gap.** At the next `*/5` tick, `watchdog.sh` sees the stale pidfile (old pre-reboot pid), spawns a fresh daemon. First refresh lands within `INTERVAL` of that — median ~2.5 min after boot, worst case ~5 min. Acceptable for a wall dashboard.
- **Decision:** kept the `@reboot` line in the crontab as belt-and-braces in case a future firmware honours it; documented the actual recovery path in [client.md](client.md#rebooting-from-the-terminal). Did not invest in switching to a proper init.d / upstart job — reboots are rare and 5 min worst-case outage is fine.

### M4.6 — Battery / mount

- [ ] Sample `lipc-get-prop com.lab126.powerd battLevel` once per loop iteration into a CSV; plot drain rate over 24h.
- [ ] Decide on wall-mount hardware + power delivery (USB cable run, dock, magsafe-style?).
- [ ] Long-soak test (24h) and capture battery drain at the production interval.

---

## Post-M4 ideas (not committed)

Pull from this list when M4 is done — don't start in parallel.

- Calendar panel (CalDAV / Google Calendar via a local sync helper)
- Kanban / chore tracker panel
- "Now playing" panel
- Multiple dashboard layouts selected via query param (`?layout=morning`, etc.)
- Configuration API the Kindle polls for refresh hints
- **Flexible refresh agenda** — generalize the two-regime day/night cadence (D15) into an arbitrary schedule: per-hour intervals, multiple windows, or a weekday/weekend split. Deferred 2026-06-13 in favour of shipping the simple two-regime version first and letting `batt.csv` show whether the extra flexibility earns its complexity.
- Support for additional eink devices at other resolutions
- Migrate `client/refresh.sh` install to a small `client/install.sh` once we have >1 client artifact
