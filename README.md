# kindle-dashboard

A self-hosted family dashboard for an old jailbroken Kindle, mounted on the living-room wall. The server (Go, packaged as a Docker image) renders dashboard images; the Kindle pulls them over local Wi-Fi and displays them with `eips`.

```
┌──────────────────────┐         HTTP GET /dashboard.png         ┌────────────────────┐
│  Kindle (7th gen)    │ ──────────────────────────────────────► │  Go server         │
│  cron + curl + eips  │ ◄────────────── 600×800 PNG ─────────── │  (Docker, VM)      │
└──────────────────────┘                                         └────────────────────┘
```

## Status

Early scaffolding. See [`docs/roadmap.md`](docs/roadmap.md) for milestones. v1 target: weather-only dashboard, rendered server-side, refreshed on a cron from the Kindle.

## Layout

- `server/` — Go HTTP server that renders PNGs (not yet created)
- `client/` — shell scripts and cron entries that run on the Kindle (not yet created)
- `docs/` — design, device notes, decisions, roadmap

## Quick links

- [Device specs & access](docs/device.md)
- [Architecture overview](docs/architecture.md)
- [Stack decisions](docs/decisions.md)
- [Client (Kindle) approach](docs/client.md)
- [Server approach](docs/server.md)
- [Roadmap](docs/roadmap.md)
