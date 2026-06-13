# Client (Kindle)

The Kindle side of the system. Two shell scripts on the device today: `loop.sh` (the M4.2 sleep+wake daemon — the thing actually driving refreshes in production) and `refresh.sh` (the original M1 one-shot, kept for manual smoke tests). See the M4.1 recon notes ([linkss](recon/2026-05-25-linkss.md), [wake investigation](recon/2026-05-25-wake-investigation.md)) and [D14](decisions.md) for why the architecture is shaped this way.

## On-device layout

All under `/mnt/us/dashboard/` (the user-writable partition; survives reboots, untouched by firmware updates):

```
/mnt/us/dashboard/
  loop.sh           # the long-running sleep+wake daemon (committed at client/loop.sh)
  watchdog.sh       # cron-driven restart-if-dead (committed at client/watchdog.sh)
  refresh.sh        # original M1 one-shot, kept for manual smoke tests (client/refresh.sh)
  config.env        # SERVER_URL, LOG_LINES, INTERVAL, night-cadence knobs — not in git, lives only on the device
  state/
    last.png        # most recent successfully fetched image
    last.log        # refresh.sh log (legacy, only written if refresh.sh is run manually)
    loop.log        # loop.sh log, auto-trimmed to LOG_LINES (default 2000)
    loop.pid        # PID of the running daemon (cleaned up on graceful exit)
    maintenance     # touch this file to put the daemon in maintenance mode (no suspend)
    batt.csv        # one row per cycle: epoch,battLevel,charging (0/1) — produced by loop.sh
    crontab.bak     # M1 snapshot of /etc/crontab/root
    crontab.m4.bak  # M4 snapshot, taken before swapping refresh.sh cron for loop.sh
```

The cron entries live in **`/etc/crontab/root`** (the system file, owned by busybox `crond` already running as PID 893 at boot). Rootfs is read-only by default, so editing this file requires `mntroot rw` / `mntroot ro` bookends.

## `loop.sh` — the sleep+wake daemon

Lives in this repo at [`../client/loop.sh`](../client/loop.sh). Long-running. Started by `@reboot` cron (and by `watchdog.sh` if its pidfile goes stale). Architecture in [D14](decisions.md); empirical findings in [recon/2026-05-25-wake-investigation.md](recon/2026-05-25-wake-investigation.md). Key design points:

- **Single-instance guard** that combines a pidfile check and a `/proc` cmdline scan. The scan catches orphan daemons whose pidfile was deleted (e.g., after a `kill -TERM` that was masked while the script was blocked in `echo mem > /sys/power/state`).
- **Prelude (once at boot):** `stop framework`, `stop lab126_gui`, `scaling_governor=powersave`, `echo enabled > rtc1/device/power/wakeup`. Without `stop framework`, `cvm` JIT crashes register wakeup events that abort every subsequent suspend.
- **Main loop:** arm `rtc1/wakealarm` → `echo mem > /sys/power/state` → on resume, LIPC wireless nudge (`wirelessEnable 0; sleep 2; wirelessEnable 1`) → poll-curl up to 20s → PNG magic check → `eips -g` → screensaver copy → battery sample to `batt.csv` → log trim → fast-return guard.
- **Fast-return guard:** if a cycle finishes in less than `INTERVAL/2` (typical cause: a USB plug or user tap waking us early), sleep the remainder before re-entering suspend. Defends against hot-spinning on stray wakeup sources.
- **Maintenance mode:** while `state/maintenance` exists, the loop polls every 30s instead of suspending. Lets the operator ssh in, edit configs, watch logs, etc. without racing the sleeper. `touch state/maintenance` to enter; `rm state/maintenance` to leave.
- **Fallback paths:** if `/sys/power/state` or the RTC's `wakealarm` aren't writable for any reason, the loop falls back to a userspace `sleep` so it still makes progress on dry-runs or off-device tests.

## Refresh cadence (and how to tweak it)

The daemon picks its sleep interval **at the top of every cycle** from the wall clock, so changes take effect on the next wake — no restart needed. Two regimes:

- **Daytime** — `INTERVAL` seconds (default **600** = 10 min).
- **Overnight** — `NIGHT_INTERVAL` seconds (default **3600** = 1 h) whenever the local hour is inside `[NIGHT_START:00, NIGHT_END:00)`. Defaults `NIGHT_START=0`, `NIGHT_END=7` → midnight to 7am.

All four are env knobs read from `config.env`. The script bakes in the defaults above, so the "10 min by day, hourly 00:00–07:00" policy is active even with no cadence lines in `config.env` at all. To change it, add/edit lines in `/mnt/us/dashboard/config.env` and let the next cycle pick them up (or `touch state/maintenance`, edit, `rm` it to apply within ~30s):

```sh
INTERVAL=900          # daytime: 15 min instead of 10
NIGHT_INTERVAL=1800   # overnight: half-hourly instead of hourly
NIGHT_START=23        # window may wrap past midnight (e.g. 23..7)
NIGHT_END=6           # back to daytime cadence at 06:00
```

Notes:

- **Disable the night slowdown:** set `NIGHT_INTERVAL` equal to `INTERVAL`, or `NIGHT_START` equal to `NIGHT_END`.
- **Wrap-around windows work:** if `NIGHT_START > NIGHT_END` (e.g. `22..7`) the window spans midnight.
- **Boundary overshoot is intentional and harmless:** the interval is chosen when the alarm is armed, so a cycle that arms at 06:30 with the default hourly night cadence sleeps until 07:30 — i.e. the switch back to daytime cadence can lag the `NIGHT_END` hour by up to one `NIGHT_INTERVAL`. Not worth clamping for a wall dashboard.
- **Ghost-refresh is cycle-counted, not time-based** (`GHOST_REFRESH_EVERY`, default 12). Overnight at hourly cycles those 12 cycles stretch across the whole night, so the full-flash de-ghost effectively pauses until morning — fine, since nothing's ghosting on a screen nobody's reading.
- **Each armed interval is logged** (`armed wakealarm for <N>s`), so `tail state/loop.log` shows the regime the daemon is actually in.

### Reaching the device while it's on a long overnight interval

Suspend means **Wi-Fi off and ssh unreachable** for the whole interval — so on the hourly night cadence the device is only ssh-reachable for the ~10–20s awake window once an hour (worst case ~1h wait). Ways in:

- **Physically wake it** (USB plug/unplug or a tap/power-button press is a wakeup source). The daemon resumes, brings Wi-Fi up, and runs its cycle; if it was woken in the first half of the interval, the **fast-return guard** then holds the device *awake* (userspace `sleep`, not suspend) for the remainder — which is exactly when ssh works. So a tap early in a night hour buys you a long ssh window.
- **Maintenance mode** is the deliberate "stay awake so I can work" switch (`touch state/maintenance`), but it requires ssh access to set — so reach for the physical wake first, then drop into maintenance mode to hold the device open.
- A clean **reboot** (`ssh kindle '/sbin/reboot'`, once you have a window) brings the framework back and the watchdog relaunches the daemon within ~5 min.

## `watchdog.sh`

Lives at [`../client/watchdog.sh`](../client/watchdog.sh). Run from cron every 5 minutes. Reads `state/loop.pid`, checks `/proc/$PID`; if alive, exits silently. If stale or missing, spawns a fresh `loop.sh` via `setsid` so it survives cron's exit.

Cron is suspended along with the kernel, so this only ever fires while the device is awake — i.e., right after a `loop.sh` wake. In the common case the pid is fresh and the watchdog exits in milliseconds.

## `refresh.sh` (legacy)

Lives in this repo at [`../client/refresh.sh`](../client/refresh.sh). Pure `/bin/sh` (busybox), no bash-isms. **No longer driven by cron** as of M4.2 — `loop.sh` replaced it. Kept on the device for manual smoke tests (`ssh kindle /mnt/us/dashboard/refresh.sh`). Key design points:

- **`PATH=/usr/sbin:/usr/bin:/bin`** at the top. The non-interactive shell crond spawns only has `/usr/bin:/bin` by default, and `eips` / `mntroot` live in `/usr/sbin`.
- **Silent failure** on network/server errors. eink retains the last frame with no power, so a failed refresh is invisible to the family in the living room. We `exit 0` from cron's perspective so it doesn't queue up failure mail.
- **Tempfile + atomic rename** before `eips -g`. No risk of drawing a half-written PNG.
- **Magic-byte check** (`89 50 4E 47 0D 0A 1A 0A`). If the server returns an HTML error page, we skip the draw instead of nuking the panel.
- **Self-trimming log** at `LOG_LINES` lines (default 500).
- **Screensaver publish** (M4.1 safety net): after a successful draw, the same PNG is copied to `/mnt/us/linkss/screensavers/bg_ss00.png`. With the M4.x sleep+wake daemon (the real solution, see [D14](decisions.md)) the stock framework is stopped and the screensaver pipeline is never engaged — this copy only matters if the daemon is off and the stock UI is back. Set `SCREENSAVER_PNG=""` to disable.
- All paths (`ROOT`) and the server URL are env-overridable so the same script handles smoke-testing without touching the production install.

## Cron entries

```
@reboot     /mnt/us/dashboard/loop.sh
*/5 * * * * /mnt/us/dashboard/watchdog.sh
```

`@reboot` brings the daemon up at boot; the watchdog catches the rare case of `loop.sh` exiting (manual kill, unexpected crash) without a reboot. The per-minute `refresh.sh` entry from M1 is gone — `loop.sh` is the only thing driving refreshes now.

busybox `crond` watches `/etc/crontab/root` and **auto-reloads on file change** — no SIGHUP or restart needed when we edit it. Confirmed on this device.

## Rebooting from the terminal

`reboot` lives at `/sbin/reboot` on the Kindle but isn't on the non-login shell's PATH, so a bare `ssh kindle reboot` returns `sh: reboot: not found`. Use the absolute path:

```sh
ssh kindle '/sbin/reboot'
```

`/sbin/halt`, `/sbin/poweroff`, and `/sbin/shutdown` all exist with the same caveat. A clean reboot is preferred over a long-press of the physical power button — it exercises the same `@reboot loop.sh` cron path the install procedure relies on, and there's no risk of holding for too long and triggering the firmware reset menu.

After reboot, expect:
- ssh comes back in roughly 30–60 seconds.
- The Kindle finishes booting into its stock UI (visible briefly on the panel).
- The `*/5 * * * * watchdog.sh` cron entry catches the stale pidfile within five minutes and relaunches `loop.sh`.
- First refresh lands roughly `(time until next */5 tick) + ~10s` after boot — median ~2.5 min, worst case ~5 min.

**`@reboot` is silently ignored on this firmware.** Busybox crond is running and the `@reboot /mnt/us/dashboard/loop.sh` entry is present, but no execution happens on boot. Verified empirically in M4.5: ssh came back at uptime 42s, crond was up (PID 853), the entry was unchanged in `/etc/crontab/root`, but no `loop.sh` process existed and the log had no new starting line. Running the watchdog manually then started the daemon correctly. The `@reboot` entry is kept in the crontab as documentation / belt-and-braces in case a future firmware honours it; the watchdog is the actual recovery mechanism.

## Maintenance mode

Working on the device by hand (editing configs, tailing logs, swapping scripts) without racing the sleeper:

```sh
ssh kindle 'touch /mnt/us/dashboard/state/maintenance'
# Daemon enters maintenance mode within one iteration. Device stays awake,
# Wi-Fi stays up, dashboard still refreshes every 30s.

# ...do work...

ssh kindle 'rm /mnt/us/dashboard/state/maintenance'
# Next iteration goes back to normal suspend cycles.
```

## Install procedure (one-time, on a fresh jailbroken Kindle)

The whole sequence, run from your host. Replace `<server>` with the dashboard server URL.

```sh
# 1. Copy both scripts into place (rootfs partition under /mnt/us/ is always writable).
scp client/loop.sh client/watchdog.sh client/refresh.sh kindle:/mnt/us/dashboard/

# 2. Drop a config (do not commit this file; it pins the server to your LAN).
ssh kindle 'cat > /mnt/us/dashboard/config.env <<EOF
SERVER_URL=http://<server>/dashboard.png
LOG_LINES=2000
INTERVAL=600
# Optional cadence knobs (defaults shown — omit to accept them):
#   NIGHT_INTERVAL=3600   # overnight refresh interval (1h)
#   NIGHT_START=0         # overnight window start hour (local)
#   NIGHT_END=7           # overnight window end hour (local, exclusive)
EOF
chmod 755 /mnt/us/dashboard/*.sh'

# 3. Sanity-check refresh.sh once manually (validates server URL + PNG path).
ssh kindle '/mnt/us/dashboard/refresh.sh && tail -3 /mnt/us/dashboard/state/last.log'

# 4. Install the cron entries. This is the only step that touches rootfs.
ssh kindle '
  export PATH=/usr/sbin:/usr/bin:/bin
  mntroot rw
  cp /etc/crontab/root /mnt/us/dashboard/state/crontab.m4.bak   # always back up first
  cp /etc/crontab/root /tmp/crontab.new
  echo "@reboot /mnt/us/dashboard/loop.sh"          >> /tmp/crontab.new
  echo "*/5 * * * * /mnt/us/dashboard/watchdog.sh"  >> /tmp/crontab.new
  mv /tmp/crontab.new /etc/crontab/root                          # atomic swap
  mntroot ro
  tail -3 /etc/crontab/root
'

# 5. Launch the daemon now (so we do not need a reboot to start the soak).
ssh kindle 'setsid /mnt/us/dashboard/loop.sh </dev/null >/dev/null 2>&1 &'

# 6. Confirm the first cycle.
ssh kindle 'sleep 6; cat /mnt/us/dashboard/state/loop.pid; tail -10 /mnt/us/dashboard/state/loop.log'
```

### Why each step is shaped that way

- **Atomic rename** (`mv /tmp/crontab.new /etc/crontab/root`): keeps the system crontab valid at all times. A half-written file would make crond skip jobs.
- **Backup** before edit: lets us roll back without re-reading the file. Restore = `mntroot rw && cp /mnt/us/dashboard/state/crontab.bak /etc/crontab/root && mntroot ro`.
- **`mntroot ro` after**: returns the system to its safe default state. The jailbreak (winterbreak2) sometimes leaves rootfs `rw` after boot, but we shouldn't rely on that — always toggle back.
- **PATH explicitly exported**: `mntroot` is in `/usr/sbin/`, not on the default non-interactive PATH.

## Rollback (uninstall)

```sh
# 1. Stop the daemon (use kill -9 — a plain TERM may be masked if the
#    process is blocked in `echo mem > /sys/power/state`).
ssh kindle 'kill -9 $(cat /mnt/us/dashboard/state/loop.pid) 2>/dev/null; rm -f /mnt/us/dashboard/state/loop.pid /mnt/us/dashboard/state/maintenance'

# 2. Restore stock cron (use the M4 backup; an even older M1 backup may also exist).
ssh kindle '
  export PATH=/usr/sbin:/usr/bin:/bin
  mntroot rw
  cp /mnt/us/dashboard/state/crontab.m4.bak /etc/crontab/root
  mntroot ro
'

# 3. Bring framework back up so the device is usable as a Kindle again.
ssh kindle 'start framework; start lab126_gui'

# 4. Optional — wipe scripts, config, state entirely.
ssh kindle 'rm -rf /mnt/us/dashboard'
```

A reboot also restores stock behaviour (framework comes back, daemon does not auto-start once `@reboot loop.sh` is removed). The CPU governor reverts to `ondemand` on reboot.

## Open items

- ~~Confirm cron survives a Kindle reboot.~~ Roadmap M4.5 — verifies `@reboot /mnt/us/dashboard/loop.sh` actually fires.
- ~~Reader UI / lock-screen overlay suppression.~~ Done in M4.2 by stopping the framework outright in `loop.sh`'s prelude.
- ~~Battery / wake-from-sleep behavior under cron-driven refresh.~~ Roadmap M4.6 — `loop.sh` already samples `battLevel` per cycle into `state/batt.csv`.
