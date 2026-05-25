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

## D6 — Image format: 600×800 8-bit grayscale PNG

**Decision:** Render to exactly the panel resolution; emit 8-bit grayscale PNGs.

**Why:**

- Recon confirmed the framebuffer is `bits_per_pixel=8 grayscale=1` (see [recon 2026-05-25](recon/2026-05-25-first-ssh.md)). The panel itself dithers to 16 visible shades, but the input format is 8-bit gray — no point pre-quantizing.
- 8-bit gray PNGs are still tiny on the wire (M2 image is ~4.5 KB).
- `eips -g <file>` accepts the format directly.

Originally written as "4-bit"; updated after on-device verification.

---

## D7 — Render on every request (no PNG cache)

**Decision:** The server re-renders the dashboard PNG on every `GET /dashboard.png`.

**Why:**

- Rendering takes ~8 ms on commodity hardware (measured locally). Cheaper than reasoning about cache invalidation.
- Means the timestamp in the image is always *now*, which is the right semantic — the client just polled, the image reflects that moment.
- Upstream data (M3 weather) gets its **own** TTL cache so we don't hammer the API. The render step is what's uncached.

---

## D8 — Server text rendering: `golang.org/x/image/font/basicfont`

**Decision:** Use `basicfont.Face7x13` for M2's text. Migrate to embedded TTF (`golang.org/x/image/font/opentype`) in M3 when the weather panel needs nicer typography.

**Why:**

- Bitmap font; no TTF file to ship in M2.
- Maintained by the Go team (low supply-chain risk, same as the rest of `golang.org/x/image`).
- 7×13 pixels reads fine at the panel's 167 PPI for a placeholder; we'll want better for production.

---

## D9 — Container: `FROM scratch`, non-root, static binary

**Decision:** Final container layer is `FROM scratch` with the static Go binary as the only file. Runs as UID/GID `65534` (`nobody`/`nogroup`).

**Why:**

- Minimal attack surface — no shell, no package manager, no libc to keep patched.
- Image is ~8 MB on disk, ~2.5 MB content, near-zero CVE footprint.
- Non-root because the server has no need for it (binds 8080, reads no protected files).
- When M3 adds HTTPS calls to Open-Meteo we'll `COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/` rather than switching to distroless — keeps the supply chain to just the Go toolchain image.

---

## D10 — Image publishing: GHCR with `latest` + `sha-<short>` tags

**Decision:** Publish `ghcr.io/daltonbr/kindle-dashboard:latest` (always the tip of `main`) and `ghcr.io/daltonbr/kindle-dashboard:sha-<short>` (one per commit) on every push to `main`. The container is intentionally vanilla; deployment specifics are the operator's call.

**Why:**

- `latest` lets the VM's compose follow the trunk with no per-release ceremony — appropriate for a single-developer hobby project.
- `sha-<short>` gives us a pin if a deploy needs rolling back, without needing to cut versioned releases.
- GitHub-provided `GITHUB_TOKEN` is enough to push to GHCR with `packages: write` — no PAT to manage.
- Repo is public, so the GHCR package can also be public — no auth on the VM side either.

If we ever need release semantics (semver tags), we can add a `v*` tag trigger to the same workflow.

---

## D11 — Branch protection: deferred

**Decision:** Branch protection on `main` is *not* enabled. CI still runs on every PR and push; failures are visible but non-blocking.

**Why:**

- Solo experimentation phase; the CI bar is for catching regressions, not gating self-merges.
- Will revisit (require CI pass + 1 review) once the project leaves the rapid-iteration phase or gains a second contributor.

---

## D12 — Open-Meteo client: single attempt, no retries

**Decision:** `weather.Client.Fetch` does one HTTP request with a 10s timeout. No retries, no exponential backoff, no jitter. Failures propagate to the caller.

**Why:**

- M3.2's TTL cache holds the last successful forecast, so transient upstream failures are already absorbed for the steady-state case.
- The first fetch (cold cache) is the only place where a single-attempt failure becomes user-visible. The dashboard handler will render a "weather unavailable" message in that case (M3.6) — acceptable for a wall display that retries on the next cron tick anyway.
- Keeps the client small and easy to reason about. Adding retries means picking a strategy (count, backoff, idempotency assumptions) and writing tests for it — disproportionate effort right now.

**Revisit when:** we see real cold-start fetch failures often enough to be annoying, or we start depending on a less-reliable upstream than Open-Meteo.
