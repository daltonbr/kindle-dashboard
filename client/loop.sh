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
: "${INTERVAL:=300}"
: "${SCREENSAVER_PNG:=/mnt/us/linkss/screensavers/bg_ss00.png}"

# Every Nth refresh cycle, use `eips -f -g` (full flashing waveform) instead
# of `eips -g` (partial refresh) to clear accumulated eink ghosting. Default
# 12 = once per hour at INTERVAL=300. Set to 0 to disable.
: "${GHOST_REFRESH_EVERY:=12}"

# Which RTC to use for the wake alarm. rtc1 = max77696-rtc.1 (PMIC channel 1).
: "${RTC:=/sys/class/rtc/rtc1}"

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

log "loop.sh starting (pid=$$, interval=${INTERVAL}s, ghost-refresh-every=${GHOST_REFRESH_EVERY})"

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

    # Arm the RTC alarm. Clear-then-set is the kernel's documented pattern.
    if [ -w "$RTC/wakealarm" ]; then
        echo 0 > "$RTC/wakealarm" 2>/dev/null || true
        echo $(( cycle_start + INTERVAL )) > "$RTC/wakealarm" 2>/dev/null \
            || log "wakealarm: failed to arm"
    else
        log "wakealarm: $RTC/wakealarm not writable — sleeping with plain sleep"
        sleep "$INTERVAL"
    fi

    # Suspend. Blocks until the alarm (or USB plug, or other wakeup source)
    # fires. On failure (e.g. /sys/power/state not writable for some
    # reason), fall back to a userspace sleep so the loop still makes
    # progress.
    if [ -w /sys/power/state ]; then
        echo mem > /sys/power/state 2>/dev/null \
            || { log "suspend failed; sleeping ${INTERVAL}s in userspace"; sleep "$INTERVAL"; }
    else
        sleep "$INTERVAL"
    fi

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
    # hot spin. Sleep the remainder of the nominal interval first.
    cycle_end=$(date +%s)
    cycle_len=$(( cycle_end - cycle_start ))
    if [ "$cycle_len" -lt $(( INTERVAL / 2 )) ]; then
        remainder=$(( INTERVAL - cycle_len ))
        log "fast-return guard: cycle ${cycle_len}s < ${INTERVAL}s/2 — sleeping ${remainder}s"
        sleep "$remainder"
    fi
done
