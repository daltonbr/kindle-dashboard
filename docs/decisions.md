# Decisions

Lightweight ADR-style log. Append to this file as we make non-obvious choices.

---

## D1 — Server language: Go

**Decision:** The server is written in Go.

**Why:**

- **Supply chain.** Go's stdlib covers everything v1 needs: `net/http`, `image`, `image/png`, `image/draw`. Zero third-party deps required to ship the MVP.
- **Single binary, easy Docker.** `FROM scratch` + a static binary ≈ a ~10 MB image with no runtime to patch.
- **The user is unfamiliar with web stacks and concerned about npm/pip supply-chain risk.** Go gives us the strongest "boring deps" story of the contenders.

**Rejected alternatives:**

- **Bun / Deno / Node.** Even Deno's permissions model still leans on a wide npm/JSR ecosystem once you need image rendering. Largest attack surface for a hobby project.
- **Python.** Pleasant to write but image rendering needs Pillow, which transitively pulls a C build chain. Smaller risk than npm but bigger than Go.
- **Shell + ImageMagick.** Fine for a single static card; rapidly painful once we want multiple panels, fonts, layout.
- **Rust.** Great supply-chain story but overkill, slower iteration, more upfront for the same outcome.

---

## D2 — Client mechanism: cron + curl + eips

**Decision:** The Kindle runs a shell script on a cron schedule that `curl`s the latest PNG and pipes it to `eips`.

**Why:**

- Standard pattern for jailbroken Kindle dashboards (matches the blog post we're following).
- No long-running process to crash or leak memory.
- Trivial to debug — we can run the script manually over SSH.
- Refresh interval is a single value in the cron entry.

**Rejected alternatives:**

- **Long-running sleep loop.** Slightly easier to layer in logic, but more failure-prone — if it dies we get a stale dashboard forever.
- **Server-push (websocket/SSE).** Overkill; reconnection logic on an ancient Kindle isn't worth it for a dashboard that updates every ~15 minutes.

---

## D3 — Weather source: Open-Meteo

**Decision:** Use [Open-Meteo](https://open-meteo.com/) as the v1 weather provider.

**Why:**

- **No API key.** No secret to manage in the repo or on the server.
- Free tier is generous; rate limits are not a concern at our polling cadence.
- Clean JSON forecast endpoint.

If we outgrow it later we can swap providers behind a `WeatherProvider` interface in the server.

---

## D4 — Network: LAN-only, no auth

**Decision:** Server listens on the Docker VM's LAN address, no authentication, no TLS.

**Why:**

- The Kindle is on the same LAN; nothing outside the LAN needs this service.
- Adding TLS to a 7th-gen Kindle's ancient curl is a fight we don't need.

**Implication:** If we ever expose this beyond the LAN — reverse proxy, VPN, anything — this decision must be revisited.

---

## D5 — Server port / hostname: deferred

**Decision:** Server binds a configurable `PORT` env var (default TBD). External port mapping decided when we write the `docker-compose.yml`, because the Docker VM has other services and 8080 may conflict.

---

## D6 — Image format: 600×800 4-bit grayscale PNG

**Decision:** Render to exactly the panel resolution and depth.

**Why:**

- Matches the 7th-gen panel native format. No resizing or dithering on the device.
- 4-bit PNG is tiny over the wire.
- `eips` accepts PNGs directly.
