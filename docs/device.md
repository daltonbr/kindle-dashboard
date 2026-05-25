# Device: Kindle 7th gen (basic touch)

The display device for this dashboard. Released 2014 — not a Paperwhite. Jailbroken.

## Hardware / firmware

| Field | Value |
| --- | --- |
| Model | Kindle 7th generation (basic touch, non-Paperwhite) |
| Firmware | Kindle 5.12.2.2 (3791510038) |
| Serial number | 90C6070654140863 |
| Wi-Fi MAC | 74:C2:46:CC:42:6C |
| Free storage | ~2.8 GB |

## Display

The 7th-gen basic Kindle ships with a **6" eink Pearl** panel:

- **Resolution: 600 × 800** (portrait)
- **Bit depth: 4-bit grayscale** (16 shades)
- **PPI: 167**
- **No frontlight** (no built-in lighting hardware)

All dashboard PNGs the server renders should target this resolution and palette. Anything outside the 16-shade grayscale palette will dither on the panel.

> If we ever swap in a Paperwhite or another eink device, document its resolution and bit depth here too — the server should be able to render to multiple sizes via a query param.

## Network access

The Kindle joins the local Wi-Fi. From the host machine:

```sh
ssh kindle
```

…uses the following `~/.ssh/config` entry:

```sshconfig
Host kindle
    Hostname 10.0.0.178
    User root
    Port 22
    IdentityFile ~/.ssh/dalton16@kindle
    IdentitiesOnly yes
    HostKeyAlgorithms +ssh-rsa
    PubkeyAcceptedAlgorithms +ssh-rsa
```

The legacy `ssh-rsa` algorithms are required because the Kindle's SSH server is old.

> **SSH server:** the jailbreak uses **dropbear** by default (lightweight SSH server common on embedded devices). An OpenSSH option exists but has not been tested yet on this device. Dropbear-specific gotchas to keep in mind:
>
> - Some `sshd_config` features (e.g. `AuthorizedKeysCommand`, `Match` blocks) don't apply.
> - Key formats and authorized-keys path may live under `/etc/dropbear/` rather than `/root/.ssh/`.
> - SFTP/scp can be limited depending on how dropbear was built — if `scp` misbehaves, fall back to `cat file | ssh kindle 'cat > /mnt/us/...'`.

## Filesystem mount mode

Rootfs is mounted **read-only by default** for safety (Kindle firmware behavior). To install scripts, edit init files, or write outside `/mnt/us/`, switch with:

```sh
mntroot rw    # remount root read-write
# ...do the install...
mntroot ro    # switch back; do this immediately when done
```

The `/mnt/us/` partition (user storage) stays writable in both modes, so anything we drop there for the dashboard does not need `mntroot rw`. We only need write mode if we touch `/etc/`, `/usr/`, or other system paths.

## Jailbreak / on-device tools

The jailbreak gives us a shell + ability to drop our own scripts under `/mnt/us/`. Tools we rely on:

- `eips` — built-in. Pushes a PNG to the eink framebuffer. Standard tool for any custom Kindle UI.
- `curl` — should be available; if not, busybox `wget` works.
- `cron` / busybox cron — exact mechanism TBD on first SSH session; see [client.md](client.md).

### Installed jailbreak hacks

- **ScreenSavers hack (`linkss`, v0.25.N by NiLuJe)** — installed and confirmed in [recon 2026-05-25](recon/2026-05-25-first-ssh.md). Modes: random cycle, shuffled cycle, **last screen**, cover. For our dashboard the **"last screen"** mode is the ideal candidate: whatever image we last wrote to the screensaver location stays on the panel during sleep. Architecture-level discussion of using this as the production refresh path lives in [architecture.md](architecture.md) ("Alt approach: linkss screensaver pipeline").
- **BatteryStatus** (KUAL extension, under `/mnt/us/extensions/BatteryStatus/`). Provides a "Print Battery Status" entry in KUAL → Helpers+. Likely a small binary that dumps battery level / temp / cycles. Worth investigating when we tackle the battery/wake question (post-M2) — could be invoked from `refresh.sh` to add a "battery: X%" line to the dashboard, and is useful baseline monitoring for the wall-mount use case.
- **MRInstaller, koreader, linkfonts, linkfonts-ovr, renameotabin, usbnet** — installed but not relevant to the dashboard pipeline.

## Things to confirm on first SSH session

These are values referenced in the blog post we're following but specific to a Paperwhite — we need to verify the equivalents on the 7th gen basic. Answered items link to the recon doc that confirmed them.

- [x] Does `eips` accept `-g` (display image) and `-c` (clear) flags the same way? — **Yes.** Binary at `/usr/sbin/eips` (not on the default non-interactive `$PATH`). See [recon 2026-05-25](recon/2026-05-25-first-ssh.md).
- [x] Path of writable storage — **`/mnt/us/`** (~2.5 GB free, `fuse.fsp` mount, writable regardless of rootfs mode).
- [x] Cron implementation available — **busybox `crond` (already running)**, root crontab at `/etc/crontab/root`.
- [x] Confirm dropbear version + whether we want to swap to OpenSSH — **Dropbear v2020.81**. Works fine; no plan to swap.
- [ ] How to disable the Kindle reader UI / lock screen so our image stays on the panel — still open; candidates noted in the recon doc.
- [ ] Battery/power behavior — does the device sleep aggressively? How do we prevent it? — still open.

Track answers as we discover them; record findings in `docs/recon/`.

## Reference

- Origin inspiration: <https://terminalbytes.com/reviving-kindle-paperwhite-7th-gen/>
  (uses a Paperwhite; some details will differ on our basic 7th gen)
