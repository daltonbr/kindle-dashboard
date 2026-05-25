# Recon: sleep + scheduled wake — 2026-05-25

Investigated as part of M4.1. Initial direction (linkss screensaver only — see
[2026-05-25-linkss](2026-05-25-linkss.md)) and a brief stop at "always-on via
`preventScreenSaver`" were both rejected in favour of **sleep most of the time,
wake briefly to refresh** — inspired by [this post on a paperwhite 7][blog].

This document captures every API probe and test result, so future work doesn't
have to re-derive any of it.

[blog]: https://terminalbytes.com/reviving-kindle-paperwhite-7th-gen/

## Goal

Build a long-running daemon on the Kindle that:

1. Spends most of its time in `mem` suspend (deep sleep, kernel frozen, sub-mA
   draw).
2. Wakes itself on schedule via the SoC RTC.
3. On wake: refresh Wi-Fi, fetch the dashboard, draw with `eips`, sleep again.

Replaces the busybox `crond`-driven `* * * * * refresh.sh` model entirely.

## Findings

### LIPC `rtcWakeup` / `wakeUp` are declared but not implemented

`lipc-probe -a com.lab126.powerd` lists `rtcWakeup` and `wakeUp` as writable
Int properties. Setting them returns:

```
com.lab126.powerd failed to set value for property rtcWakeup
  (0x8 lipcErrNoSuchProperty)
```

Same for `deferSuspend`. The blog author's `next-wakeup` helper presumably
used a different binary; this firmware version does not honour these LIPC
writes. **Do not use them.**

### `/sys/class/rtc/rtc*/wakealarm` works on all three RTCs

This Kindle exposes three RTC channels:

| sysfs path | name | backing device |
|---|---|---|
| `/sys/class/rtc/rtc0` | `max77696-rtc.0` | PMIC alarm channel 0 |
| `/sys/class/rtc/rtc1` | `max77696-rtc.1` | PMIC alarm channel 1 |
| `/sys/class/rtc/rtc2` | `snvs_rtc` | SoC secure RTC |

All three accept `echo $epoch_seconds > wakealarm` cleanly. No "Device or
resource busy" — the blog's `rtcwake` failure was a busybox-rtcwake bug, not
a kernel limitation.

Wakeup capability is enabled by default on `max77696-rtc.0` (covers both
rtc0 and rtc1 — same i2c device) and `snvs_rtc.0`. We explicitly enable
`/sys/class/rtc/rtc1/device/power/wakeup` for clarity even though the
parent already has it on.

**Standard sequence:**

```sh
echo 0 > /sys/class/rtc/rtc1/wakealarm                    # clear stale
echo $(($(date +%s) + INTERVAL)) > /sys/class/rtc/rtc1/wakealarm
echo mem > /sys/power/state    # blocks here; returns on alarm fire
```

`echo mem` returns when the kernel resumes. Verified with a single-iteration
test:

```
test ran at 19:10:22 — set wakealarm to now+60
echo mem returned at 19:11:23  ← exactly 61s later, no tap required
```

### Framework crashes break suspend

`cvm` (the Java VM running Amazon's framework — Reader UI + book download
lifecycle + AWT event loop) emits `undefined instruction` exceptions on
every resume:

```
[24694.167776] Image Fetcher 0 (3571): undefined instruction: pc=40c738a0
[24698.786208] wpa_supplicant (3649): undefined instruction: pc=40271228
[24699.913346] AWT-EventQueue- (5527): undefined instruction: pc=40c740c0
[24702.688002] LifecycleWorker (3656): undefined instruction: pc=40c79300
[24730.549067] curl (3740): undefined instruction: pc=4021c228
```

Each crash registers a wakeup event with the kernel. **The next `echo mem`
aborts immediately** because there's a pending wakeup pending — observed as
fast-return suspend cycles (~3 seconds each instead of the requested 60).

The fix is to stop the framework before entering the sleep loop:

```sh
/sbin/stop framework
/sbin/stop lab126_gui
```

(`/sbin/stop` is a symlink to `initctl`; init configs live in `/etc/init/`,
not `/etc/upstart/`. `lab126_gui_monitor` and `webreader` showed
`Unknown instance` — not running on this device.)

With framework stopped, a 5-cycle wake test ran clean:

```
iter 1: 19:34:48 → 19:35:48  (60s ✓)
iter 2: 19:35:48 → 19:36:48  (60s ✓)
iter 3: 19:36:48 → 19:37:47  (59s ✓)
iter 4: 19:37:47 → 19:38:46  (59s ✓)
iter 5: 19:38:46 → 19:39:45  (59s ✓)
```

Free memory jumps from ~50 MB to ~120 MB after the stop.

Reversibility: `start framework` brings it back live; a reboot also restores
defaults. No persistent rootfs changes.

### Wi-Fi does not reassociate on resume without the framework

With framework stopped, after suspend the `cmd` (connection manager) and
`wpa_supplicant` daemons remain alive, `wlan0` shows `UP BROADCAST RUNNING`,
and the route table looks normal — but packets don't flow. Observed during
the first post-framework-stop wake test: 8 consecutive `fetch FAILED` log
lines over 8 minutes before the device hit auto-suspend and stopped trying.

The blog's wireless toggle is the standard fix:

```sh
lipc-set-prop com.lab126.cmd wirelessEnable 0
sleep 2
lipc-set-prop com.lab126.cmd wirelessEnable 1
```

Then poll `curl` for up to ~20 seconds. In a 3-cycle integrated test the
fetch came back in **~7 seconds** of polling every time:

```
iter 1: sleep 20:36:10 → wake 20:37:08 (58s) → fetch OK after 7s polling
iter 2: sleep 20:37:17 → wake 20:38:14 (57s) → fetch OK after 7s polling
iter 3: sleep 20:38:24 → wake 20:39:21 (57s) → fetch OK after 7s polling
```

Wall-clock cycle time at a 60s nominal interval: ~67 seconds (58s suspend +
2s nudge gap + ~7s reassociation/fetch). Overhead becomes negligible at the
5–15 minute intervals we'd actually run.

### CPU governor `powersave` works and persists across resume

```sh
echo powersave > /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor
```

This Kindle (i.MX 6SoloLite) exposes only two frequencies: **984 MHz** and
**396 MHz**. `powersave` pegs at 396. Available governors: `ondemand`
(default), `userspace`, `powersave`, `performance`. Verified the setting
persists through 5 suspend/resume cycles.

### Wakeup sources currently enabled

For reference (output of `find /sys/devices -name wakeup`):

```
ENABLED: /sys/devices/platform/snvs_rtc.0/power/wakeup
ENABLED: /sys/devices/platform/imx-i2c.0/i2c-0/0-003c/power/wakeup
ENABLED: /sys/devices/platform/imx-i2c.0/i2c-0/0-003c/max77696-rtc.0/power/wakeup
ENABLED: /sys/devices/platform/fsl-ehci.0/usb1/power/wakeup
ENABLED: /sys/devices/platform/fsl-ehci.1/usb2/power/wakeup
```

USB wakeups are on — plugging the device in will also wake it from suspend.
That's fine for our use case; could be relevant if power-cycling the cable
during a soak test.

## Resulting architecture

```
prelude (once, at daemon boot):
    /sbin/stop framework
    /sbin/stop lab126_gui
    echo powersave > /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor
    echo enabled > /sys/class/rtc/rtc1/device/power/wakeup

loop forever:
    echo 0 > /sys/class/rtc/rtc1/wakealarm
    echo $(($(date +%s) + INTERVAL)) > /sys/class/rtc/rtc1/wakealarm
    echo mem > /sys/power/state         # blocks until alarm fires

    lipc-set-prop com.lab126.cmd wirelessEnable 0
    sleep 2
    lipc-set-prop com.lab126.cmd wirelessEnable 1

    # Poll-with-timeout for connectivity, then refresh as today:
    curl-with-retry $SERVER_URL > $TMP
    validate PNG magic
    mv $TMP $OUT
    eips -g $OUT
    cp $OUT $SCREENSAVER_PNG    # belt-and-suspenders for linkss
```

See [D14](../decisions.md) for the recorded decision and the rejected
alternatives. Implementation lands in M4.x — likely a new
`client/loop.sh` invoked via `@reboot` cron, with a watchdog cron that
restarts the daemon if its pidfile goes stale.

## Open questions for the implementation phase

- **Interval policy.** Start static at 5 min. Add time-of-day awareness later
  (faster during morning hours, hourly overnight, etc.).
- **Watchdog.** If `echo mem` ever fast-returns (some unexpected wakeup
  source), the loop spins hot. Guard: skip to the next interval if the cycle
  took < `INTERVAL/2`. Plus a `*/5 * * * *` cron checking the daemon's
  pidfile and restarting it if missing.
- **Crontab edit.** Need to remove the per-minute `refresh.sh` entry and add
  the `@reboot` daemon entry. Same `mntroot rw / mntroot ro` dance as the
  original install.
- **Reboot survival of `stop framework`.** Stop is runtime-only; the daemon
  re-applies it on every boot. No rootfs edits needed.
- **Battery measurement.** Need a way to read battery level over time —
  `lipc-get-prop com.lab126.powerd battLevel` works (returned 78% during
  recon). Sample once per loop iteration into a CSV for later analysis.
- **Hand-back path.** Documented in [client.md](../client.md): a reboot
  restores the Kindle to stock behaviour, and removing the cron entry stops
  the daemon from auto-starting again.
