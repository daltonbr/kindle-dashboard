# Architecture

A deliberately small system. Two halves talking over HTTP on the LAN.

```
┌────────────────────────────────────┐         ┌────────────────────────────────────┐
│ Kindle (7th gen, jailbroken)       │         │ Docker VM (Proxmox)                │
│                                    │         │                                    │
│  cron ── refresh.sh ──┐            │  HTTP   │  Go server                         │
│                       └─ curl ─────┼────────►│   GET /dashboard.png               │
│                                    │         │     ├─ fetch Open-Meteo (cached)   │
│  eips ◄── /tmp/dash.png ───────────┤         │     ├─ render 600×800 grayscale    │
│                                    │         │     └─ return image/png            │
└────────────────────────────────────┘         └────────────────────────────────────┘
```

## Why this shape

- **Server renders, client just displays.** The Kindle is slow, has tiny RAM, and an ancient toolchain. Pushing all logic server-side means we can iterate on the dashboard layout without ever touching the device.
- **Stateless HTTP.** No websockets, no MQTT, no push. The Kindle pulls on a schedule. Trivial to debug — just `curl` from any machine on the LAN.
- **No auth.** LAN-only service. If we ever expose it beyond the LAN, revisit.
- **PNG over the wire.** Native format `eips` understands. No on-device decoding gymnastics.

## Data flow (v1)

1. Kindle cron triggers `refresh.sh` every N minutes.
2. Script `curl`s `http://<server>:<port>/dashboard.png` to a temp file.
3. Script calls `eips -g <tempfile>` to draw it on the panel.
4. Server, on each request, looks at cached weather (refreshed every ~10 min from Open-Meteo) and renders a 600×800 4-bit grayscale PNG with the current dashboard layout.

## Configurability

- **Refresh interval** is owned by the client cron entry. Fast during dev (e.g. 1 min), production target ~15 min.
- **Server-side cache TTL** for upstream data (weather etc.) is independent of how often the Kindle polls — don't hammer Open-Meteo just because the client polls every minute.
- **Post-MVP:** server exposes a config endpoint so we can adjust panels / interval without re-SSHing the Kindle. The cron itself stays on the device but could pull a "should I refresh?" hint header.

## Alt approach: linkss screensaver pipeline (leading candidate post-MVP)

The Kindle ships with a "screensaver" mode (engaged on power-button tap or auto-sleep) that displays a single image full-screen with **no reader UI, no chrome, and effectively zero power draw** — the eink panel just holds the last frame. The `linkss` jailbreak extension (already installed, see [recon 2026-05-25](recon/2026-05-25-first-ssh.md)) lets us supply our own images at `/mnt/us/linkss/screensavers/`.

Why this is attractive for a wall-mounted dashboard:

- **No fight with the reader UI.** Screensaver mode is the OS's native "show one image and nothing else" state.
- **Battery.** Hours of wall-mount life vs. days if we keep the device fully awake to draw with `eips`.
- **Sleep transitions look clean** — they're the firmware's normal behavior, not a hack we layered on top.

The wrinkle: linkss doesn't auto-reload when we overwrite the file. The refresh flow becomes roughly:

1. cron fires (the device wakes briefly to run cron).
2. `curl` new PNG, atomically replace `bg_ss00.png` (or whatever name we settle on).
3. Force a screensaver re-pick or briefly wake → sleep cycle so the new image is what gets shown.
4. Back to sleep.

**We are not building this yet.** v1 (M1–M2) uses the simpler `eips -g` path so we can validate the rendering+display pipeline end-to-end as fast as possible. The decision to switch — or not — happens after we have a real dashboard image to test sleep behavior with. If we switch, this becomes the production setup; `eips -g` stays around for dev/debug.

## Failure modes worth designing for

- **Server down.** Client `curl` should fail silently and leave the previous image on screen. eink keeps the last frame with no power.
- **Wi-Fi drop.** Same — silent fail, retry next interval.
- **Bad image.** Validate the PNG (non-empty file, correct header) before calling `eips`, otherwise we may blank the screen.
- **Kindle sleeps / lock screen takes over.** TBD how aggressively to fight this; depends on what we find on the device.
