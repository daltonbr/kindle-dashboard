# AGENTS.md

Onboarding for agents (and humans) joining this repo cold. Read this once, then dive into the specific doc you need.

## What this is

A self-hosted family dashboard for an old jailbroken **Kindle 7th gen** (basic touch, non-Paperwhite) mounted on the living-room wall. A Go server renders 600×800 grayscale PNGs; the Kindle pulls them on a cron and draws them with `eips`. The server runs in Docker, packaged from `ghcr.io/daltonbr/kindle-dashboard`.

## Current state

| Milestone | State |
| --- | --- |
| **M1** — Client pipeline (cron + `refresh.sh` + `eips`) | ✅ done, live on the device |
| **M2** — Minimal Go server + Dockerfile + CI + GHCR publish | ✅ done, rendering on the panel |
| **M3** — Weather panel (Open-Meteo) | ✅ done, live on the panel |
| **M4** — Polish + reliability (sleep/wake, battery, prod cadence) | ✅ effectively done — M4.1–4.5 done (D15 cadence confirmed live on device 2026-06-14); refresh-freeze bug fixed 2026-06-14 (fast-return guard no longer plain-sleeps; all idle via `suspend_for()`, [D21]); **M4.6 (battery/mount) deferred** (deprioritized 2026-06-14, hardware-led, non-blocking) |
| **M5** — Composable widgets (2×2 grid, data-layer providers) | ✅ done — three weather cards on a 2×2 grid from a typed data layer, live Open-Meteo provider (default, hourly precip + 3-day daily), layout decisions server-side (footer rain, `DASHBOARD_ORIENTATION`), deployed on the panel. First private source (was M5.5) carried forward. See `docs/widgets.md`, decisions [D16]/[D17] |
| **M6** — Calendar (first private source) | ✅ done — live on the panel (deployed + verified 2026-06-14). `gitleaks` CI guard [D18]; `CalendarAgenda` card in the bottom-left cell, fed by a Google Calendar secret iCal URL [D19] behind the `data` seam; stdlib ICS parser + bounded-horizon RRULE, `time/tzdata` embedded [D20]. Inert without `CALENDAR_ICS_URL`. Soak/monitoring for real-feed edge cases. See `docs/roadmap.md` M6 |

**Next up: no milestone chosen yet.** M6 is soaking on the panel; pick the next
widget/capability from the post-M4 ideas in `docs/roadmap.md` once it looks clean.
**M4.6 (battery/mount) is deferred** (deprioritized 2026-06-14, hardware-led, not
a blocker).

See `docs/roadmap.md` for sub-task breakdowns, and `docs/widgets.md` for the M5 widget architecture.

## Repo layout

```
.
├── AGENTS.md                ← you are here
├── README.md
├── client/
│   └── refresh.sh           # Kindle-side script (busybox /bin/sh, no bash-isms)
├── server/
│   ├── main.go              # net/http, slog, graceful shutdown, provider wiring
│   ├── internal/data/       # typed models + providers (WeatherModel, DemoWeather, OpenMeteo)
│   ├── internal/render/     # Dashboard(model, Options) -> *image.Gray; grid + page chrome
│   ├── internal/render/widgets/  # Widget interface + WeatherToday/WeatherForecast/Rain
│   ├── internal/weather/    # Open-Meteo client + TTL cache (M3)
│   ├── Dockerfile           # multi-stage, FROM scratch, non-root
│   └── go.mod / go.sum
├── .github/workflows/
│   ├── ci.yml               # vet, golangci-lint v2, test -race, build, shellcheck
│   └── release.yml          # build & push to ghcr.io on push to main
├── docs/
│   ├── device.md            # Kindle hardware, ssh, jailbreak hacks
│   ├── architecture.md      # system shape + linkss alt path
│   ├── client.md            # install procedure + cron + rollback
│   ├── server.md            # endpoints, env vars, deploy contract
│   ├── decisions.md         # ADR-style decision log (D1-D16)
│   ├── widgets.md           # M5 widget architecture: grid, data layer, separation
│   ├── roadmap.md           # M0-M5 with sub-tasks
│   └── recon/               # dated, frozen recon snapshots
└── cspell.json
```

## Common commands

```sh
# Server: build/test/lint loop
cd server
go mod tidy
go vet ./...
go test -race ./...
go build .
PORT=8765 ./server                       # run locally

# Docker
docker build -t kindle-dashboard-server:dev ./server
docker run --rm -p 8765:8080 kindle-dashboard-server:dev

# Client: shellcheck + scp install
shellcheck client/*.sh
scp client/refresh.sh kindle:/mnt/us/dashboard/refresh.sh

# Kindle reachability
ssh kindle 'date; tail -3 /mnt/us/dashboard/state/last.log'
```

## Project conventions

**Communication style:**
- The user wants to be in the loop on risky / public-facing steps (`mntroot rw`, system file edits, repo visibility, force pushes, branch protection). Confirm before doing those.
- Go slow when the user is involved. Short status updates, not silent runs.
- Commit messages: explain the "why", not the "what". No `Co-Authored-By` lines (per user's global preference).

**Repo is public** as of M2. Do not put operator-internal infrastructure details (deployment tooling names, hypervisor, internal hostnames, etc.) in committed docs. LAN IPs are fine. The device IP `10.0.0.178` and the Mac dev IP `10.0.0.184` already exist in past docs.

**Code style:**
- **Server (Go):** stdlib first. One outside dep allowed: `golang.org/x/image/font/basicfont` (M2) → opentype + embedded TTF (M3). Anything else needs a new entry in `docs/decisions.md`.
- **Client (shell):** pure `/bin/sh` (busybox-compatible). No bash-isms. Silent failure on network/server errors (eink retains the last frame). Atomic tempfile + rename before `eips`. PATH must be set explicitly because non-interactive ssh on the Kindle defaults to `/usr/bin:/bin` and `eips` / `mntroot` live in `/usr/sbin`.
- **Docs:** new decisions append to `decisions.md` with **Why** + alternatives. Recon docs in `docs/recon/` are dated snapshots — frozen, never edited after creation.

**Spelling:** `cspell.json` at repo root carries the dictionary. Add domain words there (e.g. `eips`, `eink`, `dropbear`, `linkss`) rather than spread `// cspell:ignore` everywhere.

## Domain knowledge worth knowing up front

- **Display:** 600×800, 8 bpp grayscale framebuffer (panel dithers to 16 visible shades). Render PNGs at exactly that size, 8-bit gray (`*image.Gray`).
- **Kindle ssh:** `ssh kindle` works. Server is dropbear v2020.81. Legacy `ssh-rsa` required.
- **Rootfs is read-only by default.** Use `mntroot rw` / `mntroot ro` (located at `/usr/sbin/mntroot`) to bracket any edit under `/etc/` or `/usr/`. `/mnt/us/` is always writable.
- **eips path issue:** `/usr/sbin/eips` is NOT on the default non-interactive `$PATH`. Set `PATH=/usr/sbin:/usr/bin:/bin` in any script.
- **busybox crond is already running (PID 893).** Root crontab at `/etc/crontab/root`. busybox crond auto-reloads on file change — no SIGHUP needed.
- **Sleep behavior:** the Kindle drops to deep sleep after ~30 min idle. Wi-Fi off, ssh unreachable, cron may fire but `curl` fails. Solved in M4.2 by `loop.sh` (controlled `echo mem` suspend + RTC wake; see `docs/client.md`, [D14]).
- **`powerd` is never stopped — and it idle-suspends behind your back.** `loop.sh` stops `framework` and `lab126_gui` but not `powerd`, so powerd still autonomously drives the panel into its stock screensaver and suspends it after a few idle minutes. The daemon only stays in control because its `echo mem` suspend wins the race; **any userspace `sleep` longer than powerd's idle timeout will be suspended mid-count and freeze** (`nanosleep` on `CLOCK_MONOTONIC` doesn't advance across suspend). This froze the dashboard for ~8h on 2026-06-14 — fixed by routing all idle through `suspend_for()` ([D21]).
- **Jailbreak inventory:** linkss (screensaver hack — supports "last screen" mode, the leading post-MVP candidate for the refresh path), BatteryStatus, KUAL, MRInstaller, koreader, usbnet, linkfonts.
- **Open-Meteo** is the M3 weather source. No API key required.

## Open questions deliberately not answered yet

These are recorded throughout the docs but the headlines:

1. **How to keep the dashboard visible 24/7** without fighting the reader UI / lock-screen. Leading candidate: switch refresh path to write to `/mnt/us/linkss/screensavers/*.png` and put linkss in "last screen" mode. Decision: deferred to M4.
2. **Battery / wake-from-sleep behavior** at our cron cadence. Whether to use `lipc-set-prop` to prevent sleep, or accept that the dashboard only refreshes during waking moments (and lean on the screensaver pipeline). M4.
3. **Does `/etc/crontab/root` survive a Kindle firmware update?** Unverified. Low risk for now.
4. **Future cron edits:** factor the install procedure in `docs/client.md` into a small `client/install.sh` script when we have ≥2 client artifacts to install. Not yet.

## What NOT to do

- **Do not** commit a `Co-Authored-By` trailer to git messages.
- **Do not** put operator-internal infra in docs — see "Repo is public" above.
- **Do not** modify or delete `docs/recon/*.md` — they're dated snapshots.
- **Do not** lower `go 1.26` in `go.mod` (user wants latest Go for security patches; tool support is the variable to fix instead).
- **Do not** `mntroot rw` and forget to `mntroot ro` after.
- **Do not** add a long userspace `sleep` to `loop.sh` — it freezes when powerd suspends the device mid-sleep. Idle only via `suspend_for()` ([D21]).
- **Do not** flip repo settings (visibility, branch protection, package visibility) without explicit user confirmation in the current turn — those are durable admin changes.
- **Do not** treat memory recall as authoritative; verify on disk before acting on it.

## When picking up next session

1. Read this file.
2. Skim `docs/roadmap.md` for the current milestone.
3. Glance at `docs/decisions.md` for live-decision rationale.
4. `ssh kindle 'tail -5 /mnt/us/dashboard/state/last.log'` to confirm the pipeline is still live.
5. Ask the user what they want to do — don't assume from past context that anything is half-finished.
