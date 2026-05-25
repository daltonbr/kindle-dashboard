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

## Failure modes worth designing for

- **Server down.** Client `curl` should fail silently and leave the previous image on screen. eink keeps the last frame with no power.
- **Wi-Fi drop.** Same — silent fail, retry next interval.
- **Bad image.** Validate the PNG (non-empty file, correct header) before calling `eips`, otherwise we may blank the screen.
- **Kindle sleeps / lock screen takes over.** TBD how aggressively to fight this; depends on what we find on the device.
