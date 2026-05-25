#!/bin/sh
# kindle-dashboard refresh script
#
# Runs on a Kindle 7th gen (basic touch), invoked by busybox crond.
# Fetches a PNG from the dashboard server and draws it to the eink panel.
#
# Design notes:
#   - Pure /bin/sh (busybox). No bash-isms.
#   - eips is at /usr/sbin/eips; the non-interactive PATH doesn't include
#     /usr/sbin, so we set PATH explicitly.
#   - Silent failure on network/server errors. eink retains the last frame
#     with no power, so a stale dashboard is the right failure mode for a
#     family device on a wall.
#   - Atomic rename: never let eips read a half-written PNG.
#   - Magic-byte check: if the server returns HTML (e.g. an error page),
#     don't feed it to eips.

set -u

PATH=/usr/sbin:/usr/bin:/bin
export PATH

# --- config ----------------------------------------------------------------

# Where everything related to the dashboard lives on the device.
# Overridable via env (useful for smoke-testing without touching /mnt/us/).
: "${ROOT:=/mnt/us/dashboard}"

# Server endpoint. Override by editing this line or sourcing a config file
# alongside the script (kept separate so secrets/IPs stay out of git).
# shellcheck source=/dev/null
[ -r "$ROOT/config.env" ] && . "$ROOT/config.env"
: "${SERVER_URL:=http://CHANGE-ME:PORT/dashboard.png}"

# Number of log lines to keep (older ones are trimmed on each run).
: "${LOG_LINES:=500}"

# Path to the linkss (NiLuJe screensavers hack) backing file. Kept up to date
# so the framework's screensaver pipeline (when active) shows the last good
# dashboard rather than the 2021 placeholder PNG. With the M4.1 sleep+wake
# daemon, the framework is stopped and this code path is a safety net only.
# Set SCREENSAVER_PNG="" to disable. See docs/recon/2026-05-25-linkss.md.
: "${SCREENSAVER_PNG:=/mnt/us/linkss/screensavers/bg_ss00.png}"

OUT="$ROOT/state/last.png"
TMP="$OUT.tmp"
LOG="$ROOT/state/last.log"

mkdir -p "$ROOT/state"

# --- logging ---------------------------------------------------------------

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*" >> "$LOG"; }

# Keep the log from growing without bound: trim to last $LOG_LINES lines on each run.
if [ -f "$LOG" ]; then
    tail -n "$LOG_LINES" "$LOG" > "$LOG.trim" 2>/dev/null && mv "$LOG.trim" "$LOG"
fi

# --- fetch -----------------------------------------------------------------

if ! curl -fsS --max-time 20 -o "$TMP" "$SERVER_URL"; then
    log "fetch FAILED from $SERVER_URL"
    rm -f "$TMP"
    exit 0
fi

# PNG magic bytes: 89 50 4E 47 0D 0A 1A 0A
# Read first 8 bytes and compare. od is busybox-provided.
MAGIC=$(od -An -N8 -tx1 "$TMP" 2>/dev/null | tr -d ' \n')
if [ "$MAGIC" != "89504e470d0a1a0a" ]; then
    log "bad PNG magic: got '$MAGIC' from $SERVER_URL"
    rm -f "$TMP"
    exit 0
fi

# --- swap + draw -----------------------------------------------------------

mv "$TMP" "$OUT"

if eips -g "$OUT" >/dev/null 2>&1; then
    log "ok"
else
    log "eips FAILED to draw $OUT"
fi

# Publish the same PNG as the linkss screensaver image so the dashboard
# stays visible after the device sleeps. Failure here is non-fatal — the
# foreground refresh already succeeded.
if [ -n "$SCREENSAVER_PNG" ] && [ -d "$(dirname "$SCREENSAVER_PNG")" ]; then
    if ! cp "$OUT" "$SCREENSAVER_PNG" 2>/dev/null; then
        log "screensaver copy FAILED to $SCREENSAVER_PNG"
    fi
fi
