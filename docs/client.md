# Client (Kindle)

The Kindle side of the system: a tiny shell script + cron entry.

## Files we expect to live on the Kindle

All under `/mnt/us/dashboard/` (the user-writable partition):

```
/mnt/us/dashboard/
  refresh.sh        # fetch + display
  config.env        # SERVER_URL, polling cadence is in cron not here
  state/
    last.png        # last successfully fetched image (cache)
    last.log        # tail-only log
```

Cron entry lives in whatever scheduler the jailbroken firmware ships (TBD on first session — likely a `crond` from busybox or KUAL/MRPI's helper).

## refresh.sh — sketch

> Not yet committed. This is the target shape; we'll harden it once we've actually SSH'd in and confirmed available tools.

```sh
#!/bin/sh
set -eu

SERVER_URL="${SERVER_URL:-http://docker-vm.local:PORT}/dashboard.png"
OUT=/mnt/us/dashboard/state/last.png
TMP="${OUT}.tmp"

# Fetch with a sensible timeout. Silent fail leaves the previous frame on the panel.
if curl -fsS --max-time 20 -o "$TMP" "$SERVER_URL"; then
    # Sanity-check it's a PNG before showing it
    if head -c 8 "$TMP" | od -An -c | grep -q 'P   N   G'; then
        mv "$TMP" "$OUT"
        eips -g "$OUT"
    fi
fi
rm -f "$TMP"
```

Key design points:

- **Silent failure on network/server errors.** eink retains the last frame with no power, so a failed refresh is invisible to the family in the living room.
- **Tempfile + atomic rename.** No risk of `eips` reading a half-written PNG.
- **Magic-byte check.** If the server ever returns an HTML error page, we don't want to feed that to `eips`.

## Cron entry — sketch

```
# every 1 minute during dev:
* * * * * /mnt/us/dashboard/refresh.sh >> /mnt/us/dashboard/state/last.log 2>&1

# production target:
*/15 * * * * /mnt/us/dashboard/refresh.sh >> /mnt/us/dashboard/state/last.log 2>&1
```

## Open questions (resolve on first SSH session)

- Which cron daemon ships with this jailbreak? Does it survive reboots?
- Where exactly should the script live so it's not wiped by a Kindle update?
- How to disable the reader UI / suspend the screensaver so our image is what actually shows on the panel.
- Does `eips` here support the same flags as the Paperwhite version in the blog post (`-g`, `-c`, `-f`)?

See [device.md](device.md) for the checklist.
