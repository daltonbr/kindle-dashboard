# Roadmap

Milestones, roughly in order. Each one ends with a working, demonstrable thing — no half-states.

## M0 — Repo bootstrap ✅

- [x] Local git repo
- [x] Initial docs (`device.md`, `architecture.md`, `decisions.md`, `client.md`, `server.md`, `roadmap.md`)
- [x] README

## M1 — Client display pipeline (no real server yet)

Goal: a static PNG, served from any HTTP source, ends up on the Kindle panel via cron.

- [ ] SSH into the Kindle, answer the open questions in [device.md](device.md)
- [ ] Confirm `eips` flags and writable paths
- [ ] Write `client/refresh.sh` and install it to `/mnt/us/dashboard/`
- [ ] Stand up a cron entry and verify it survives a reboot
- [ ] Test with a hand-rolled 600×800 PNG served from any laptop on the LAN (or `python -m http.server`)

**Definition of done:** the PNG visibly updates on the Kindle when we change the file the server returns.

## M2 — Minimal Go server returning a static dashboard

- [ ] `server/main.go` with `GET /dashboard.png` returning a hardcoded "Hello, Kindle" 600×800 grayscale PNG
- [ ] `go.mod`, Dockerfile (multi-stage, `FROM scratch`), and `docker-compose.yml`
- [ ] Deploy to the Docker VM, decide the external port
- [ ] Point the Kindle's `refresh.sh` at it

**Definition of done:** the Kindle shows a server-rendered image, deployed via compose.

## M3 — Weather panel (Open-Meteo)

- [ ] Open-Meteo client in `server/internal/weather/`
- [ ] In-memory TTL cache (don't refetch more than every ~10 min)
- [ ] Weather panel renderer with current temp, conditions icon (or text), short forecast
- [ ] Configurable `WEATHER_LAT` / `WEATHER_LON` via env

**Definition of done:** weather information shows on the Kindle and updates within `WEATHER_TTL` of a real-world change.

## M4 — Polish + reliability

- [ ] Production cron cadence (~15 min)
- [ ] Server healthcheck endpoint + Docker healthcheck
- [ ] Log retention on the Kindle (rotate `state/last.log`)
- [ ] Kindle screensaver / reader UI handled cleanly
- [ ] Battery & charging plan for living-room mount

## Post-MVP ideas (not committed)

- Calendar panel (CalDAV / Google Calendar via a local sync helper)
- Kanban / chore tracker panel
- "Now playing" panel
- Multiple dashboard layouts selected via query param (`?layout=morning`, etc.)
- Configuration API the Kindle polls for refresh hints
- Support for additional eink devices at other resolutions

Pull from this list when M4 is done — don't start in parallel.
