# kindle-dashboard

A self-hosted family dashboard for an old jailbroken Kindle, mounted on the living room wall. The server (Go, packaged as a Docker image) renders dashboard images; the Kindle pulls them over local Wi-Fi and displays them with `eips`.

```
┌──────────────────────┐         HTTP GET /dashboard.png         ┌────────────────────┐
│  Kindle (7th gen)    │ ──────────────────────────────────────► │  Go server         │
│  cron + curl + eips  │ ◄────────────── 600×800 PNG ─────────── │  (Docker, VM)      │
└──────────────────────┘                                         └────────────────────┘
```

## Status

See [`docs/roadmap.md`](docs/roadmap.md) for milestones.
We have a working MVP running on a device, the server is fetching the weather forecast from OpenMeteo.

## Layout

- `server/` — Go HTTP server that renders PNGs
- `client/` — shell scripts and cron entries that run on the Kindle
- `docs/` — design, device notes, decisions, roadmap

## Quick links

- [AGENTS.md](AGENTS.md) — onboarding instructions
- [Device specs & access](docs/device.md)
- [Architecture overview](docs/architecture.md)
- [Stack decisions](docs/decisions.md)
- [Client (Kindle) approach](docs/client.md)
- [Server approach](docs/server.md)
- [Roadmap](docs/roadmap.md)
