# Client (Kindle)

The Kindle side of the system: a tiny shell script invoked by busybox `crond`. As of M1, this is live on the device — see [recon 2026-05-25](recon/2026-05-25-first-ssh.md) for the facts that shaped it.

## On-device layout

All under `/mnt/us/dashboard/` (the user-writable partition; survives reboots, untouched by firmware updates):

```
/mnt/us/dashboard/
  refresh.sh        # the fetch+display script (committed to repo at client/refresh.sh)
  config.env        # SERVER_URL, LOG_LINES — not in git, lives only on the device
  state/
    last.png        # most recent successfully fetched image
    last.log        # script log, auto-trimmed to LOG_LINES on each run
    crontab.bak     # one-time snapshot of /etc/crontab/root before our edit
```

The cron entry itself lives in **`/etc/crontab/root`** (the system file, owned by busybox `crond` which is already running as PID 893 at boot). Rootfs is read-only by default, so editing this file requires `mntroot rw` / `mntroot ro` bookends.

## `refresh.sh`

Lives in this repo at [`../client/refresh.sh`](../client/refresh.sh). Pure `/bin/sh` (busybox), no bash-isms. Key design points:

- **`PATH=/usr/sbin:/usr/bin:/bin`** at the top. The non-interactive shell crond spawns only has `/usr/bin:/bin` by default, and `eips` / `mntroot` live in `/usr/sbin`.
- **Silent failure** on network/server errors. eink retains the last frame with no power, so a failed refresh is invisible to the family in the living room. We `exit 0` from cron's perspective so it doesn't queue up failure mail.
- **Tempfile + atomic rename** before `eips -g`. No risk of drawing a half-written PNG.
- **Magic-byte check** (`89 50 4E 47 0D 0A 1A 0A`). If the server returns an HTML error page, we skip the draw instead of nuking the panel.
- **Self-trimming log** at `LOG_LINES` lines (default 500).
- **Screensaver publish** (M4.1 safety net): after a successful draw, the same PNG is copied to `/mnt/us/linkss/screensavers/bg_ss00.png`. With the M4.x sleep+wake daemon (the real solution, see [D14](decisions.md)) the stock framework is stopped and the screensaver pipeline is never engaged — this copy only matters if the daemon is off and the stock UI is back. Set `SCREENSAVER_PNG=""` to disable.
- All paths (`ROOT`) and the server URL are env-overridable so the same script handles smoke-testing without touching the production install.

## Cron entry

Current dev cadence — every minute:

```
* * * * * /mnt/us/dashboard/refresh.sh
```

Production target: `*/15 * * * *`.

busybox `crond` watches `/etc/crontab/root` and **auto-reloads on file change** — no SIGHUP or restart needed when we edit it. Confirmed on this device.

## Install procedure (one-time, on a fresh jailbroken Kindle)

The whole sequence, run from your host. Replace `<server>` with the dashboard server URL.

```sh
# 1. Copy script into place (rootfs partition under /mnt/us/ is always writable).
scp client/refresh.sh kindle:/mnt/us/dashboard/refresh.sh

# 2. Drop a config (do not commit this file; it pins the server to your LAN).
ssh kindle 'cat > /mnt/us/dashboard/config.env <<EOF
SERVER_URL=http://<server>/dashboard.png
LOG_LINES=500
EOF
chmod 755 /mnt/us/dashboard/refresh.sh'

# 3. Sanity-check by running it once manually.
ssh kindle '/mnt/us/dashboard/refresh.sh && tail -3 /mnt/us/dashboard/state/last.log'

# 4. Install the cron entry. This is the only step that touches rootfs.
ssh kindle '
  export PATH=/usr/sbin:/usr/bin:/bin
  mntroot rw
  cp /etc/crontab/root /mnt/us/dashboard/state/crontab.bak    # always back up first
  cp /etc/crontab/root /tmp/crontab.new
  echo "* * * * * /mnt/us/dashboard/refresh.sh" >> /tmp/crontab.new
  mv /tmp/crontab.new /etc/crontab/root                       # atomic swap
  mntroot ro
  tail -3 /etc/crontab/root
'

# 5. Wait ~70s and confirm cron fired it.
ssh kindle 'sleep 75; tail -3 /mnt/us/dashboard/state/last.log'
```

### Why each step is shaped that way

- **Atomic rename** (`mv /tmp/crontab.new /etc/crontab/root`): keeps the system crontab valid at all times. A half-written file would make crond skip jobs.
- **Backup** before edit: lets us roll back without re-reading the file. Restore = `mntroot rw && cp /mnt/us/dashboard/state/crontab.bak /etc/crontab/root && mntroot ro`.
- **`mntroot ro` after**: returns the system to its safe default state. The jailbreak (winterbreak2) sometimes leaves rootfs `rw` after boot, but we shouldn't rely on that — always toggle back.
- **PATH explicitly exported**: `mntroot` is in `/usr/sbin/`, not on the default non-interactive PATH.

## Rollback (uninstall)

```sh
ssh kindle '
  export PATH=/usr/sbin:/usr/bin:/bin
  mntroot rw
  cp /mnt/us/dashboard/state/crontab.bak /etc/crontab/root
  mntroot ro
'
ssh kindle 'rm -rf /mnt/us/dashboard'   # optional — wipes script, config, state
```

## Open items (deferred to post-M2)

- Confirm cron survives a Kindle reboot. The crontab is in rootfs and should — but untested.
- ~~Reader UI / lock-screen overlay suppression. Leading candidate is the `linkss` screensaver pipeline.~~ Done in M4.1 via screensaver publish — see [recon 2026-05-25-linkss](recon/2026-05-25-linkss.md).
- Battery / wake-from-sleep behavior under cron-driven refresh.

These do not block server work and are best decided once we have a real dashboard image to test sleep behavior against.
