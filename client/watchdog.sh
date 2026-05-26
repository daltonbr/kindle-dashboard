#!/bin/sh
# kindle-dashboard daemon watchdog
#
# Run from cron every few minutes (`*/5 * * * *`). If loop.sh's pidfile
# is missing or its pid is no longer a running process, relaunch the
# daemon. No-op otherwise.
#
# Safe to run while loop.sh is suspending the device: cron is suspended
# along with the kernel, so this script only ever fires when the device
# is awake — and the awake window is exactly when loop.sh is active and
# its pidfile is fresh, so the common case is a quick exit.

set -u

PATH=/usr/sbin:/usr/bin:/bin
export PATH

: "${ROOT:=/mnt/us/dashboard}"

PIDFILE="$ROOT/state/loop.pid"
LOG="$ROOT/state/loop.log"
LOOP="$ROOT/loop.sh"

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] watchdog: $*" >> "$LOG"; }

if [ -r "$PIDFILE" ]; then
    PID=$(cat "$PIDFILE" 2>/dev/null)
    if [ -n "$PID" ] && [ -d "/proc/$PID" ]; then
        exit 0
    fi
    log "stale pidfile (pid=$PID not running) — relaunching"
else
    log "no pidfile — relaunching"
fi

if [ ! -x "$LOOP" ]; then
    log "loop.sh missing or not executable at $LOOP — giving up"
    exit 0
fi

# Detach: redirect FDs so cron doesn't wait on us, and run in a new
# session so the daemon survives cron's exit.
setsid "$LOOP" </dev/null >/dev/null 2>&1 &
log "spawned new loop.sh (pid=$!)"
