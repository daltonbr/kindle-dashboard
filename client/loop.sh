#!/bin/sh
# kindle-dashboard sleep+wake daemon
#
# Runs on a Kindle 7th gen. Replaces the per-minute crond entry of M1 with
# a long-running loop that suspends the device between refreshes and uses
# the SoC RTC alarm to wake up. See docs/decisions.md (D14) and
# docs/recon/2026-05-25-wake-investigation.md for the architecture and
# the empirical findings that drove every choice in this script.
#
# Lifecycle: started by `@reboot` cron (and by watchdog.sh if the pidfile
# goes stale). Survives across resume — the only persistent state is the
# pidfile and the log.

set -u

PATH=/usr/sbin:/usr/bin:/bin
export PATH

# --- config ----------------------------------------------------------------

: "${ROOT:=/mnt/us/dashboard}"

# shellcheck source=/dev/null
[ -r "$ROOT/config.env" ] && . "$ROOT/config.env"

: "${SERVER_URL:=http://CHANGE-ME:PORT/dashboard.png}"
: "${LOG_LINES:=2000}"

# Daytime refresh interval, in seconds. This is the knob you'll most often
# want to tweak (e.g. 600 -> 900 to trade freshness for battery). Overridable
# in config.env.
: "${INTERVAL:=600}"

# --- time-of-day interval policy -------------------------------------------
#
# Between NIGHT_START:00 and NIGHT_END:00 (local wall-clock, exclusive of the
# end hour) the daemon refreshes every NIGHT_INTERVAL seconds instead of
# INTERVAL. Nobody's looking at a wall dashboard at 03:00, so we trade
# freshness for battery overnight. The interval is recomputed at the top of
# every cycle, so a change to config.env takes effect on the next wake.
#
# Defaults below implement "10 min by day, once an hour from midnight to 7am".
# To tweak later, set any of these in /mnt/us/dashboard/config.env:
#   INTERVAL=900            # slower daytime cadence (15 min)
#   NIGHT_INTERVAL=1800     # half-hourly overnight instead of hourly
#   NIGHT_START=23          # window may wrap past midnight, e.g. 23..7
#   NIGHT_END=6             # wake back to daytime cadence at 06:00
# To DISABLE the night slowdown entirely, set NIGHT_INTERVAL equal to INTERVAL
# (or NIGHT_START equal to NIGHT_END).
: "${NIGHT_INTERVAL:=3600}"
: "${NIGHT_START:=0}"
: "${NIGHT_END:=7}"

: "${SCREENSAVER_PNG:=/mnt/us/linkss/screensavers/bg_ss00.png}"

# Every Nth refresh cycle, use `eips -f -g` (full flashing waveform) instead
# of `eips -g` (partial refresh) to clear accumulated eink ghosting. Default
# 12 = once per hour at INTERVAL=300. Set to 0 to disable.
: "${GHOST_REFRESH_EVERY:=12}"

# Which RTC to use for the wake alarm. rtc1 = max77696-rtc.1 (PMIC channel 1).
: "${RTC:=/sys/class/rtc/rtc1}"

# If `echo mem` returns in fewer than this many seconds, the suspend did not
# stick (it was aborted by an asserted wake source — on this device that's
# almost always a connected USB cable, which blocks deep suspend). Used by
# suspend_for() to detect the aborted-suspend case and pace the remainder in a
# userspace sleep instead of hot-spinning a full fetch+redraw loop. Every
# legitimate suspend in this daemon is hundreds of seconds, so 10 cleanly
# separates "stuck" from "bounced straight back out". See [D22].
: "${SUSPEND_STUCK_MIN:=10}"

# CPU governor to set at boot. "powersave" pegs the i.MX 6SoloLite at
# 396 MHz; "ondemand" is the firmware default.
: "${GOVERNOR:=powersave}"

# Seconds between the wireless-disable and wireless-enable LIPC writes.
WIRELESS_NUDGE_GAP=2
# Max seconds to poll for connectivity after the nudge before giving up
# on this iteration.
FETCH_POLL_TIMEOUT=20
# Maintenance-mode poll interval: when the maintenance flag file exists,
# the daemon skips suspend and sleeps this many seconds between checks
# so the device stays awake (Wi-Fi up, ssh reachable, framework still
# stopped) for as long as the operator needs it.
MAINTENANCE_POLL=30

OUT="$ROOT/state/last.png"
TMP="$OUT.tmp"
LOG="$ROOT/state/loop.log"
PIDFILE="$ROOT/state/loop.pid"
MAINT="$ROOT/state/maintenance"
BATTCSV="$ROOT/state/batt.csv"

mkdir -p "$ROOT/state"

# --- logging ---------------------------------------------------------------

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*" >> "$LOG"; }

trim_log() {
    [ -f "$LOG" ] || return 0
    tail -n "$LOG_LINES" "$LOG" > "$LOG.trim" 2>/dev/null && mv "$LOG.trim" "$LOG"
}

# --- single-instance guard -------------------------------------------------
#
# Two checks: the pidfile (cheap, fast path) and a /proc cmdline scan
# (catches orphan daemons whose pidfile was rm'd, e.g. after a TERM that
# was masked during `echo mem > /sys/power/state`).
#
# The /proc scan matches the script path as argv[1] specifically — not
# anywhere in the cmdline — so we don't false-positive on shells whose
# command body happens to mention the script (e.g. a deploy script run
# via ssh heredoc).

for d in /proc/[0-9]*; do
    [ -d "$d" ] || continue
    other=${d##*/}
    [ "$other" = "$$" ] && continue
    [ -r "$d/cmdline" ] || continue
    # argv[1] is the second null-separated field in /proc/PID/cmdline.
    other_argv1=$(tr '\0' '\n' < "$d/cmdline" 2>/dev/null | sed -n '2p')
    if [ "$other_argv1" = "$0" ]; then
        log "another loop.sh is already running as pid $other — exiting"
        exit 0
    fi
done

echo $$ > "$PIDFILE"

cleanup() {
    rm -f "$PIDFILE"
}
trap cleanup EXIT INT TERM

# --- prelude (once, at daemon boot) ----------------------------------------
#
# These are runtime-only — a reboot restores stock behaviour. See D14 for why
# each is needed. Errors are logged but not fatal; the loop can survive a
# partial prelude (e.g. governor write failing) better than no daemon at all.

log "loop.sh starting (pid=$$, interval=${INTERVAL}s, night-interval=${NIGHT_INTERVAL}s in [${NIGHT_START}:00,${NIGHT_END}:00), ghost-refresh-every=${GHOST_REFRESH_EVERY})"

# Counter used by the ghost-refresh policy. Starts at 1 so the first cycle
# (cycle_count == 1) does a partial refresh; the first full flash is on
# cycle GHOST_REFRESH_EVERY itself.
cycle_count=0

# 1. Stop the framework. Without this, cvm's `undefined instruction` JIT
#    crashes on every resume register wakeup events that abort the next
#    suspend (observed: ~3s fast-return cycles vs requested 60s).
/sbin/stop framework   >/dev/null 2>&1 || log "stop framework: already stopped or failed"
/sbin/stop lab126_gui  >/dev/null 2>&1 || log "stop lab126_gui: already stopped or failed"

# 2. CPU governor. Persists across resume; only needs to be set once.
if [ -w /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor ]; then
    echo "$GOVERNOR" > /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2>/dev/null \
        || log "governor: failed to set $GOVERNOR"
fi

# 3. Make sure the chosen RTC can wake the system. The parent i2c device
#    already has wakeup enabled by default; this is belt-and-braces.
if [ -w "$RTC/device/power/wakeup" ]; then
    echo enabled > "$RTC/device/power/wakeup" 2>/dev/null || true
fi

# --- fetch helpers ---------------------------------------------------------

fetch_once() {
    # Build URL with the current battery telemetry as optional query params.
    # The server treats `batt` as the trigger to render the widget — when
    # BATT_LEVEL is empty (e.g. lipc-get-prop failed), no params are appended
    # and the widget doesn't render.
    url="$SERVER_URL"
    if [ -n "${BATT_LEVEL:-}" ]; then
        sep="?"
        case $url in *\?*) sep="&" ;; esac
        url="${url}${sep}batt=${BATT_LEVEL}&plug=${BATT_CHARGING:-0}"
    fi
    curl -fsS --max-time 10 -o "$TMP" "$url"
}

fetch_with_poll() {
    # Poll fetch_once for up to FETCH_POLL_TIMEOUT seconds. Returns 0 on
    # the first success, non-zero if the whole window elapses.
    start=$(date +%s)
    while :; do
        if fetch_once; then
            elapsed=$(( $(date +%s) - start ))
            log "fetch ok after ${elapsed}s"
            return 0
        fi
        now=$(date +%s)
        if [ $(( now - start )) -ge "$FETCH_POLL_TIMEOUT" ]; then
            log "fetch FAILED after ${FETCH_POLL_TIMEOUT}s polling"
            return 1
        fi
        sleep 1
    done
}

validate_png() {
    MAGIC=$(od -An -N8 -tx1 "$TMP" 2>/dev/null | tr -d ' \n')
    [ "$MAGIC" = "89504e470d0a1a0a" ]
}

draw() {
    mv "$TMP" "$OUT"
    # Periodic full-flash refresh clears accumulated eink ghosting. Partial
    # refresh (the default `eips -g`) leaves faint after-images from prior
    # frames; -f forces a full flashing waveform that resets the pixels.
    if [ "$GHOST_REFRESH_EVERY" -gt 0 ] && [ $(( cycle_count % GHOST_REFRESH_EVERY )) -eq 0 ]; then
        log "ghost-refresh cycle ($cycle_count) — using full flash"
        eips -f -g "$OUT" >/dev/null 2>&1 || log "eips -f FAILED to draw $OUT"
    else
        eips -g "$OUT" >/dev/null 2>&1 || log "eips FAILED to draw $OUT"
    fi
    if [ -n "$SCREENSAVER_PNG" ] && [ -d "$(dirname "$SCREENSAVER_PNG")" ]; then
        cp "$OUT" "$SCREENSAVER_PNG" 2>/dev/null \
            || log "screensaver copy FAILED to $SCREENSAVER_PNG"
    fi
}

suspend_for() {
    # Idle for $1 seconds, preferring a real RTC-woken suspend but degrading
    # gracefully when the device refuses to suspend.
    #
    # The hard constraint: a plain userspace `sleep` uses CLOCK_MONOTONIC
    # (busybox nanosleep), which does NOT advance while the device is
    # suspended. powerd (still running — we only stop framework and
    # lab126_gui) idle-suspends after a few minutes, so a userspace sleep that
    # outlasts powerd's idle timer freezes mid-count if a real suspend happens
    # under it. That stalled the loop for ~8h on 2026-06-14 [D21].
    #
    # The complementary failure (2026-06-14, [D22]): when a USB cable is
    # connected the device CANNOT enter deep suspend at all — `echo mem`
    # returns in ~1s. Blindly trusting it turned the loop into a hot-spin
    # (full fetch + eips + Wi-Fi nudge every ~19s) that drained ~10%/hr.
    #
    # The two failures are mutually exclusive: the freeze needs a real suspend
    # to happen under the sleep; the hot-spin happens precisely because no
    # suspend can happen. So: try the real suspend; if it didn't stick, the
    # remainder is safe to wait out in a userspace sleep (nothing can suspend
    # us mid-count) — and that's far cheaper than re-fetching immediately.
    secs=$1
    [ "$secs" -gt 0 ] || return 0
    now=$(date +%s)

    if [ ! -w "$RTC/wakealarm" ] || [ ! -w /sys/power/state ]; then
        # Can't arm the alarm or can't suspend — pace in userspace. Safe: if a
        # real suspend is impossible here, powerd can't suspend us mid-sleep.
        log "suspend unavailable — userspace sleep ${secs}s"
        sleep "$secs"
        return
    fi

    echo 0 > "$RTC/wakealarm" 2>/dev/null || true
    echo $(( now + secs )) > "$RTC/wakealarm" 2>/dev/null || log "wakealarm: failed to arm"
    log "armed wakealarm for ${secs}s"

    # wakeup_count handshake (kernel's documented pattern): echo the current
    # count back before suspending so a wake event that arrived in the gap
    # aborts the write cleanly instead of letting us enter and immediately
    # bounce out. Best-effort — not all aborts are caught this way.
    if [ -r /sys/power/wakeup_count ]; then
        wc=$(cat /sys/power/wakeup_count 2>/dev/null)
        [ -n "$wc" ] && echo "$wc" > /sys/power/wakeup_count 2>/dev/null
    fi

    t0=$(date +%s)
    echo mem > /sys/power/state 2>/dev/null || log "echo mem: write rejected"
    slept=$(( $(date +%s) - t0 ))

    # Did the suspend stick? If `echo mem` returned far earlier than the alarm,
    # it was aborted (USB connected / wake source asserted). Wait out the
    # remainder in userspace rather than returning to hot-spin the fetch loop.
    if [ "$slept" -lt "$SUSPEND_STUCK_MIN" ]; then
        remainder=$(( secs - slept ))
        if [ "$remainder" -gt 0 ]; then
            log "suspend did not stick (${slept}s); USB likely connected — userspace sleep ${remainder}s"
            sleep "$remainder"
        fi
    fi
}

read_battery() {
    # Populate BATT_LEVEL (0-100) and BATT_CHARGING (0/1) from LIPC. Both
    # may end up empty if the properties aren't available on this firmware
    # — fetch_once handles that by skipping the query params entirely.
    BATT_LEVEL=$(lipc-get-prop com.lab126.powerd battLevel 2>/dev/null)
    BATT_CHARGING=0
    if [ "$(lipc-get-prop com.lab126.powerd isCharging 2>/dev/null)" = "1" ]; then
        BATT_CHARGING=1
    fi
}

sample_battery() {
    [ -n "${BATT_LEVEL:-}" ] || return 0
    echo "$(date +%s),${BATT_LEVEL},${BATT_CHARGING:-0}" >> "$BATTCSV"
}

# --- time-of-day interval ---------------------------------------------------

in_night_window() {
    # $1 = current hour as an integer 0-23. Returns 0 (true) if that hour is
    # inside the overnight window. Handles a window that wraps past midnight
    # (NIGHT_START > NIGHT_END, e.g. 23..7) as well as the simple case.
    h=$1
    if [ "$NIGHT_START" -le "$NIGHT_END" ]; then
        [ "$h" -ge "$NIGHT_START" ] && [ "$h" -lt "$NIGHT_END" ]
    else
        [ "$h" -ge "$NIGHT_START" ] || [ "$h" -lt "$NIGHT_END" ]
    fi
}

current_interval() {
    # %H is zero-padded ("03", "09"); strip the leading zero so busybox sh
    # arithmetic/comparison doesn't choke on "08"/"09" as invalid octal.
    hour=$(date +%H)
    hour=${hour#0}
    : "${hour:=0}"
    if in_night_window "$hour"; then
        echo "$NIGHT_INTERVAL"
    else
        echo "$INTERVAL"
    fi
}

# --- main loop -------------------------------------------------------------

while :; do
    cycle_start=$(date +%s)
    cycle_count=$(( cycle_count + 1 ))

    # Maintenance mode: while the flag file exists, skip suspend entirely
    # and just refresh on a short polling interval. Lets the operator
    # ssh in, edit configs, watch logs, etc. without racing the sleeper.
    # `touch $ROOT/state/maintenance` to enter; `rm` to leave.
    if [ -e "$MAINT" ]; then
        log "maintenance mode — skipping suspend, sleeping ${MAINTENANCE_POLL}s"
        sleep "$MAINTENANCE_POLL"
        # Still do a refresh so the dashboard stays current while the
        # operator works. Wi-Fi is already up (no suspend happened), so
        # skip the wireless nudge.
        read_battery
        if fetch_with_poll && validate_png; then
            draw
        else
            rm -f "$TMP"
        fi
        sample_battery
        trim_log
        continue
    fi

    # Pick this cycle's interval from the wall clock (daytime vs overnight).
    # Computed fresh every cycle so a config.env edit or the day/night
    # boundary is picked up on the next wake without restarting the daemon.
    interval=$(current_interval)

    # Suspend until the RTC alarm (or USB plug, or other wakeup source) fires.
    suspend_for "$interval"

    wake_at=$(date +%s)
    slept=$(( wake_at - cycle_start ))
    log "wake after ${slept}s"

    # Wi-Fi nudge. Without the framework, wlan0 stays UP but routes don't
    # carry packets after resume; this LIPC pair forces a reassociation.
    lipc-set-prop com.lab126.cmd wirelessEnable 0 >/dev/null 2>&1 || true
    sleep "$WIRELESS_NUDGE_GAP"
    lipc-set-prop com.lab126.cmd wirelessEnable 1 >/dev/null 2>&1 || true

    read_battery

    if fetch_with_poll; then
        if validate_png; then
            draw
        else
            log "bad PNG magic from $SERVER_URL"
            rm -f "$TMP"
        fi
    else
        rm -f "$TMP"
    fi

    sample_battery
    trim_log

    # Fast-return guard. If something woke us early (USB plug, stray
    # wakeup source), don't immediately re-enter suspend — that risks a
    # hot spin. Wait out the remainder of the nominal interval first.
    cycle_end=$(date +%s)
    cycle_len=$(( cycle_end - cycle_start ))
    if [ "$cycle_len" -lt $(( interval / 2 )) ]; then
        remainder=$(( interval - cycle_len ))
        log "fast-return guard: cycle ${cycle_len}s < ${interval}s/2 — re-suspending for ${remainder}s"
        # Brief userspace cooldown to ride out a transient asserted wake
        # source, then re-suspend properly. The cooldown is deliberately
        # short (5s): a long userspace sleep here would hand the idle window
        # to powerd, which idle-suspends the panel and freezes the sleep
        # (nanosleep doesn't advance across suspend) — the bug that stalled
        # the loop for ~8h on 2026-06-14. See suspend_for().
        sleep 5
        remainder=$(( remainder - 5 ))
        [ "$remainder" -gt 0 ] && suspend_for "$remainder"
    fi
done
