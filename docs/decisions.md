# Decisions

Lightweight ADR-style log. Append to this file as we make non-obvious choices.

---

## D1 — Server language: Go

**Decision:** The server is written in Go.

**Why:**

- **Supply chain.** Go's stdlib covers everything v1 needs: `net/http`, `image`, `image/png`, `image/draw`. Zero third-party deps required to ship the MVP.
- **Single binary, easy Docker.** `FROM scratch` + a static binary ≈ a ~10 MB image with no runtime to patch.
- **The user is unfamiliar with web stacks and concerned about npm/pip supply-chain risk.** Go gives us the strongest "boring deps" story of the contenders.

**Rejected alternatives:**

- **Bun / Deno / Node.** Even Deno's permissions model still leans on a wide npm/JSR ecosystem once you need image rendering. Largest attack surface for a hobby project.
- **Python.** Pleasant to write but image rendering needs Pillow, which transitively pulls a C build chain. Smaller risk than npm but bigger than Go.
- **Shell + ImageMagick.** Fine for a single static card; rapidly painful once we want multiple panels, fonts, layout.
- **Rust.** Great supply-chain story but overkill, slower iteration, more upfront for the same outcome.

---

## D2 — Client mechanism: cron + curl + eips

**Decision:** The Kindle runs a shell script on a cron schedule that `curl`s the latest PNG and pipes it to `eips`.

**Why:**

- Standard pattern for jailbroken Kindle dashboards (matches the blog post we're following).
- No long-running process to crash or leak memory.
- Trivial to debug — we can run the script manually over SSH.
- Refresh interval is a single value in the cron entry.

**Rejected alternatives:**

- **Long-running sleep loop.** Slightly easier to layer in logic, but more failure-prone — if it dies we get a stale dashboard forever.
- **Server-push (websocket/SSE).** Overkill; reconnection logic on an ancient Kindle isn't worth it for a dashboard that updates every ~15 minutes.

---

## D3 — Weather source: Open-Meteo

**Decision:** Use [Open-Meteo](https://open-meteo.com/) as the v1 weather provider.

**Why:**

- **No API key.** No secret to manage in the repo or on the server.
- Free tier is generous; rate limits are not a concern at our polling cadence.
- Clean JSON forecast endpoint.

If we outgrow it later we can swap providers behind a `WeatherProvider` interface in the server.

---

## D4 — Network: LAN-only, no auth

**Decision:** Server listens on the Docker VM's LAN address, no authentication, no TLS.

**Why:**

- The Kindle is on the same LAN; nothing outside the LAN needs this service.
- Adding TLS to a 7th-gen Kindle's ancient curl is a fight we don't need.

**Implication:** If we ever expose this beyond the LAN — reverse proxy, VPN, anything — this decision must be revisited.

---

## D5 — Server port / hostname: deferred

**Decision:** Server binds a configurable `PORT` env var (default TBD). External port mapping decided when we write the `docker-compose.yml`, because the Docker VM has other services and 8080 may conflict.

---

## D6 — Image format: 600×800 8-bit grayscale PNG

**Decision:** Render to exactly the panel resolution; emit 8-bit grayscale PNGs.

**Why:**

- Recon confirmed the framebuffer is `bits_per_pixel=8 grayscale=1` (see [recon 2026-05-25](recon/2026-05-25-first-ssh.md)). The panel itself dithers to 16 visible shades, but the input format is 8-bit gray — no point pre-quantizing.
- 8-bit gray PNGs are still tiny on the wire (M2 image is ~4.5 KB).
- `eips -g <file>` accepts the format directly.

Originally written as "4-bit"; updated after on-device verification.

---

## D7 — Render on every request (no PNG cache)

**Decision:** The server re-renders the dashboard PNG on every `GET /dashboard.png`.

**Why:**

- Rendering takes ~8 ms on commodity hardware (measured locally). Cheaper than reasoning about cache invalidation.
- Means the timestamp in the image is always *now*, which is the right semantic — the client just polled, the image reflects that moment.
- Upstream data (M3 weather) gets its **own** TTL cache so we don't hammer the API. The render step is what's uncached.

---

## D8 — Server text rendering: `basicfont` (M2) → `opentype` + Atkinson Hyperlegible (M3.4)

**Decision:** M2 used `basicfont.Face7x13` as a no-asset placeholder. M3.4 swapped to an embedded Atkinson Hyperlegible TTF via `golang.org/x/image/font/opentype`. See [D13](#d13--font-atkinson-hyperlegible-embedded).

**Why:**

- Bitmap font; no TTF file to ship in M2.
- Maintained by the Go team (low supply-chain risk, same as the rest of `golang.org/x/image`).
- 7×13 pixels reads fine at the panel's 167 PPI for a placeholder; we'll want better for production.

---

## D9 — Container: `FROM scratch`, non-root, static binary

**Decision:** Final container layer is `FROM scratch` with the static Go binary as the only file. Runs as UID/GID `65534` (`nobody`/`nogroup`).

**Why:**

- Minimal attack surface — no shell, no package manager, no libc to keep patched.
- Image is ~8 MB on disk, ~2.5 MB content, near-zero CVE footprint.
- Non-root because the server has no need for it (binds 8080, reads no protected files).
- When M3 adds HTTPS calls to Open-Meteo we'll `COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/` rather than switching to distroless — keeps the supply chain to just the Go toolchain image.

---

## D10 — Image publishing: GHCR with `latest` + `sha-<short>` tags

**Decision:** Publish `ghcr.io/daltonbr/kindle-dashboard:latest` (always the tip of `main`) and `ghcr.io/daltonbr/kindle-dashboard:sha-<short>` (one per commit) on every push to `main`. The container is intentionally vanilla; deployment specifics are the operator's call.

**Why:**

- `latest` lets the VM's compose follow the trunk with no per-release ceremony — appropriate for a single-developer hobby project.
- `sha-<short>` gives us a pin if a deploy needs rolling back, without needing to cut versioned releases.
- GitHub-provided `GITHUB_TOKEN` is enough to push to GHCR with `packages: write` — no PAT to manage.
- Repo is public, so the GHCR package can also be public — no auth on the VM side either.

If we ever need release semantics (semver tags), we can add a `v*` tag trigger to the same workflow.

---

## D11 — Branch protection: deferred

**Decision:** Branch protection on `main` is *not* enabled. CI still runs on every PR and push; failures are visible but non-blocking.

**Why:**

- Solo experimentation phase; the CI bar is for catching regressions, not gating self-merges.
- Will revisit (require CI pass + 1 review) once the project leaves the rapid-iteration phase or gains a second contributor.

---

## D12 — Open-Meteo client: single attempt, no retries

**Decision:** `weather.Client.Fetch` does one HTTP request with a 10s timeout. No retries, no exponential backoff, no jitter. Failures propagate to the caller.

**Why:**

- M3.2's TTL cache holds the last successful forecast, so transient upstream failures are already absorbed for the steady-state case.
- The first fetch (cold cache) is the only place where a single-attempt failure becomes user-visible. The dashboard handler will render a "weather unavailable" message in that case (M3.6) — acceptable for a wall display that retries on the next cron tick anyway.
- Keeps the client small and easy to reason about. Adding retries means picking a strategy (count, backoff, idempotency assumptions) and writing tests for it — disproportionate effort right now.

**Revisit when:** we see real cold-start fetch failures often enough to be annoying, or we start depending on a less-reliable upstream than Open-Meteo.

---

## D13 — Font: Atkinson Hyperlegible (embedded)

**Decision:** Ship `AtkinsonHyperlegible-Regular.ttf` embedded into the binary via `//go:embed`. Use `golang.org/x/image/font/opentype` to produce per-size faces, cached.

**Why:**

- **Designed for low-vision readability** — the Braille Institute's brief was differentiating commonly confused glyphs (slashed 0, distinct I/l/1). That's exactly what we want at a 600×800 panel viewed from across a room.
- **Crisp at small sizes.** Renders cleanly at the ~12–16 px label sizes we need for the chart axis without going to subpixel territory the eink panel can't show anyway.
- **Open Font License.** Permissive, ships with the binary, no runtime fetch.
- **`golang.org/x/image/font/opentype`** is already in the dep graph (we were using `basicfont` from the same module). No new top-level dependency.

**Layout sizes (current):** 110 px for the big current temp, 32 for the condition word, 28 for the page header, 18 for body, 12–14 for labels.

**Asset hygiene:** `OFL.txt` lives next to the TTF in `server/internal/render/fonts/`. Required by the license.

**Rejected alternatives:**

- **Inter / IBM Plex Sans.** Both excellent, both also OFL. Inter is denser (more text per row); Plex feels more "techie". Either would work; Atkinson wins on legibility-at-distance, which matters most for a wall display.
- **System font discovery / no embed.** Would need `fontconfig` or hand-coded paths inside a `FROM scratch` image where neither exists. Embedding wins on simplicity.

---

## D14 — Sleep + scheduled wake (path A+wake)

**Decision:** The dashboard runs as a long-running daemon that suspends the
device between refreshes using a sysfs RTC wakealarm. The cron-driven
`refresh.sh` model is retired in favour of this daemon. Implemented in M4.2 as
[`client/loop.sh`](../client/loop.sh) and [`client/watchdog.sh`](../client/watchdog.sh);
the architecture and the per-API findings that justify it are in
[recon 2026-05-25-wake-investigation](recon/2026-05-25-wake-investigation.md).

**Sketch (see recon for full detail):**

```
prelude:
    stop framework / lab126_gui
    scaling_governor = powersave
    enable wakeup on /sys/class/rtc/rtc1

loop:
    echo $((now + INTERVAL)) > /sys/class/rtc/rtc1/wakealarm
    echo mem > /sys/power/state                # blocks; resumes on alarm
    lipc-set-prop com.lab126.cmd wirelessEnable 0/1   # nudge Wi-Fi
    refresh dashboard, eips -g, copy to bg_ss00.png
```

**Why this and not "always on":**

- **Days vs hours of battery.** The K7 in deep `mem` suspend draws sub-mA;
  always-on with `preventScreenSaver=1` keeps Wi-Fi associated and `cvm`
  active, burning hours of battery per day. The dashboard is wall-mounted
  but the user prefers it to be unplugged-tolerant.
- **Empirical validation.** Three-cycle integrated test landed clean 60s
  suspends with HTTP fetches succeeding ~7s after each wake. See recon.
- **The blog's pattern works on this firmware.** With one substitution: the
  `rtcWakeup` LIPC property declared by `powerd` is actually unimplemented
  (returns `lipcErrNoSuchProperty`); the sysfs wakealarm path is the real
  API.
- **Framework stop is a hard prerequisite, not a power optimisation.** The
  `cvm` Java processes (Image Fetcher, AWT-EventQueue, LifecycleWorker,
  AdmDaemon) crash with `undefined instruction` on every resume, and each
  crash registers a wakeup event that aborts the next `echo mem` in
  milliseconds. Stopping the framework removes the crash cascade.
- **Wi-Fi nudge is the standard fix.** Without the framework's nudges, the
  `cmd`/`wpa_supplicant` daemons stay running but `wlan0` doesn't route
  packets after resume. Toggling `wirelessEnable` brings it back in ~7s.

**Reversibility / hand-back path:**

- `stop framework` is runtime-only. A reboot restores the stock Kindle UI.
- Removing the daemon's cron entry (`@reboot`) stops it from auto-starting.
- linkss screensaver publish stays in `refresh.sh` as a safety net for the
  fallback case where the daemon is off and the stock framework is active.

**Implementation notes (added in M4.2):**

- **Single-instance guard** combines a pidfile check with a `/proc` cmdline
  scan. The pidfile alone is insufficient because signals (TERM/KILL) sent to
  the daemon while it is blocked in `echo mem > /sys/power/state` are masked
  by the kernel — a manual `kill` followed by `rm` on the pidfile and a
  fresh launch produced two concurrent daemons during M4.2 deploy. The
  cmdline scan catches that case.
- **Maintenance mode** via a flag file (`state/maintenance`). When present,
  the loop polls every 30s instead of suspending, so the operator can ssh in
  to edit configs/scripts/cron without racing the sleeper. Without this, a
  5-minute suspend window forces a hard tap-to-wake to recover ssh access.

**Rejected alternatives:**

- **`preventScreenSaver`-driven always-on.** Tested empirically — works, but
  burns battery and doesn't give us the "days on a single charge" payoff
  that was the main motivation for choosing an eink Kindle for the wall.
- **`linkss` screensaver only.** Image stays visible during sleep (eink
  hold), but cron is suspended along with the kernel — refreshes only happen
  when someone walks by and taps. Content stales by up to ~10 min between
  interactions. See [recon 2026-05-25-linkss](recon/2026-05-25-linkss.md).
- **LIPC `rtcWakeup`/`wakeUp`.** Declared in `lipc-probe -a` output but
  return `lipcErrNoSuchProperty` on write. Dead API on this firmware.
- **busybox `rtcwake`.** Returns "Device or resource busy" — likely the bug
  the blog hit. Direct sysfs writes work; we don't need the userspace tool.

**Revisit when:** we observe the wake-nudge sequence dropping a refresh in
practice; we want sub-5-minute refresh cadence (the overhead becomes a
larger fraction of the cycle); or we need a different wake source (calendar
events, push notifications) than fixed-interval polling.

---

## D15 — Time-of-day refresh cadence

**Decision:** `loop.sh` chooses its sleep interval per-cycle from the local
wall clock: `INTERVAL` (default 600s / 10 min) by day, `NIGHT_INTERVAL`
(default 3600s / 1 h) when the local hour is in `[NIGHT_START, NIGHT_END)`
(default `0..7`, i.e. midnight–7am). All four are `config.env` env knobs;
the script's built-in defaults already implement the policy with no config
present. Closes the M4.3 "schedule-aware interval" follow-on.

**Why:**

- **Battery, for free.** Nobody reads a living-room wall dashboard at 03:00.
  Dropping from 6 wakes/hour to 1 across a 7-hour window removes ~35
  wake/Wi-Fi-reassociate/fetch cycles per night — the most expensive part of
  each cycle is the post-resume Wi-Fi reassociation, not the suspend itself.
- **The daemon already knows the time.** It's awake and running `date` at the
  moment it arms the next alarm, so picking the interval there is a couple of
  lines — no new wake source, no scheduler, no server involvement.
- **Recompute-per-cycle over compute-once.** Reading the clock every cycle
  means a `config.env` edit or the day/night boundary is honoured on the next
  wake without restarting the daemon. The cost is one `date` call per cycle —
  negligible.

**Trade-offs accepted:**

- **Boundary overshoot.** The interval is fixed when the alarm is armed, so
  the morning switch back to daytime cadence can lag `NIGHT_END` by up to one
  `NIGHT_INTERVAL` (arm at 06:30 → next wake 07:30). Not worth clamping for a
  wall display.
- **Coarser overnight ssh access.** During an hourly night cycle the device
  is ssh-reachable only in the ~10–20s awake window once an hour. Mitigated by
  physical wake + the fast-return guard's awake-remainder window, and by
  maintenance mode. Documented in [client.md](client.md#refresh-cadence-and-how-to-tweak-it).
- **Ghost-refresh pauses overnight.** `GHOST_REFRESH_EVERY` counts cycles, not
  wall-time, so the periodic full-flash de-ghost effectively stops until
  morning. Acceptable — nothing ghosts on a screen nobody's looking at.

**Rejected alternatives:**

- **Server-driven cadence** (Kindle polls a `?next=` hint). More moving parts,
  couples client cadence to server availability, and the client already has
  the only input it needs (the clock). Kept on the post-M4 idea list.
- **Sunrise/sunset-aware window** via the Open-Meteo data we already fetch.
  Cute, but a fixed clock window is what the user asked for and is trivially
  predictable. Revisit if a fixed window proves annoying around solstices.

**Revisit when:** we want more than two regimes (e.g. a faster morning burst),
a weekday/weekend split, or sunrise-relative timing.

---

## D16 — Widget data layer: in-repo providers, secrets via env

**Decision:** The M5 widget architecture keeps all data-source integration code
**in this (public) repo**, behind per-domain provider interfaces
(`WeatherProvider`, later `CalendarProvider`, `HomeAssistantProvider`). Secrets
(API keys, tokens) and personal identifiers (calendar IDs, HA entity IDs/URL,
exact coords) live **only in the deployment environment** (env vars / a
host-only file), never in git and never in the image. No separate "data
sidecar" service and no private Go module. Full design in
[widgets.md](widgets.md).

**Why:**

- **Public repos are safe when they contain no secret material.** Security comes
  from externalised config, not from hiding code. The client already follows
  this (`config.env` is gitignored, lives only on the device); we extend the
  same discipline to the server.
- **Simplest thing that meets the constraints.** With native iOS/macOS widgets
  dropped as a goal (the main driver for a shared JSON data layer), a sidecar's
  second service + JSON contract buys nothing here. In-repo interfaces + env
  config is less to build, deploy, and version.
- **The interface seam is the part that actually matters.** A widget renders
  from a typed model and never knows the source; providers are swappable
  (`Demo*` for tests, real impls in prod). That decoupling — not the physical
  location of the code — is what enables modular widget development.

**Safety practices (required for any provider that needs a secret):**

- Secret/personal config via env or a mounted file only; documented in
  [server.md](server.md#secret-hygiene--config-public-repo) with placeholder
  values exclusively.
- Providers are **inert without config** — return an error (widget shows
  "unavailable") or fall back to their `Demo*` sibling. A clone with no config
  does nothing private and CI/forks run clean.
- The GHCR image stays secret-free (`FROM scratch`, binary only); secrets enter
  at `docker run` time (`--env-file` on the host).
- Enable GitHub secret scanning + push protection (free on public repos); add a
  `gitleaks` CI step / pre-commit hook before the first private source (M5.5).

**Revisit when:** native iOS/macOS widgets become a real goal (→ reconsider the
sidecar/JSON data layer), or a provider needs config too rich/personal to keep
comfortably in env (→ reconsider going private).
