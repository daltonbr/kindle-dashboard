# Widget architecture (M5) — design + plan

Status: **complete (2026-06-14).** Three weather cards on a filling 2×2 grid,
both rain placements, the live Open-Meteo provider (M5.3, hourly precip + 3-day
daily) wired as the default, and layout decisions moved server-side (M5.6) so the
device fetches a bare `/dashboard.png`. The build is a
deliberate **redesign**, not the byte-for-byte port this doc originally sketched;
see [D17](decisions.md#d17--m5-widget-build-redesign-over-byte-for-byte-port-three-weather-cards-on-a-filling-22)
for what changed and the [step checklist](#next-steps-m5-checklist) for current
state. The sketches below remain the conceptual intent.

## Intent

Move the dashboard from one hard-coded layout to **small composable widgets**
arranged on a grid, so that:

1. **Development is modular** — each widget is built and tested in isolation
   against demo data, independent of the data sources behind it. *(Primary goal.)*
2. **Account/key-based integrations stay decoupled** from rendering, behind a
   provider interface, and **never leak secrets** even though the repo is public
   — see [Separation & safety](#separation--safety) and
   [D16](decisions.md#d16--widget-data-layer-in-repo-providers-secrets-via-env).

The Kindle still fetches a single composed PNG from `GET /dashboard.png`. The
grid is entirely server-side.

**Non-goal:** native iOS/macOS widgets. Considered and set aside — it was the
main reason a separate JSON "data sidecar" looked attractive; without it, the
simplest model (everything in this repo, secrets via env) wins.

## Decisions locked (2026-06-13)

| Decision | Choice |
| --- | --- |
| Grid | **2×2 quadrants + spans.** Footprints: 1×1 (quarter), 2×1 / 1×2 (half), 2×2 (full). *As built: cells **fill** the area between fixed ~84px header/footer bands rather than being forced square — see [D17](decisions.md#d17--m5-widget-build-redesign-over-byte-for-byte-port-three-weather-cards-on-a-filling-22).* |
| Orientation | **Portrait default (600×800)**; landscape (800×600) via `?orientation=landscape`. Grid is 2 cols × 2 rows; cell size derives from orientation. 1×1 widgets scale their content to the cell height so landscape's smaller cells don't overflow. |
| Layout selection | **Single static layout for now.** Multi-layout switching (`?layout=`, time-of-day) deferred until widgets are proven. |
| Pilot data source | **Weather**, kept as the first widget. Develop against **demo/hardcoded data first**, then wire a real provider. Candidates: keep **Open-Meteo** (no key, already integrated, multi-day forecast) and/or add **OpenWeatherMap** (freemium, API key via env) to exercise the key-based path. |
| Separation | **In-repo provider interfaces; secrets + personal config via env.** No sidecar, no private module. Public repo is safe because it contains zero secret material — see [D16](decisions.md#d16--widget-data-layer-in-repo-providers-secrets-via-env). |

## Three layers

```
┌──────────────────────────────────────────────────────────┐
│ Render server (this repo, public)                         │
│                                                            │
│  data layer ──► grid/layout ──► widgets ──► composed PNG   │
│  (typed models)  (footprint→rect)  (draw into a Rect)      │
└──────────────────────────────────────────────────────────┘
```

### 1. Widgets (render)

A widget draws a typed model into a rectangle. The existing
`panels.Weather(dst *image.Gray, area image.Rectangle, forecast)` is already
exactly this shape — it's the seed for the `Widget` interface.

```go
// sketch — names TBD
type Widget interface {
    // Footprint in grid cells: {Cols, Rows} ∈ {1,2}×{1,2}.
    Footprint() (cols, rows int)
    // Draw into the pixel rect the grid assigned. Must not draw outside it.
    Render(dst *image.Gray, area image.Rectangle)
}
```

A widget is constructed *with* its data model (so `Render` takes no data arg),
which keeps the grid agnostic about widget-specific types.

### 2. Grid / layout (placement)

Maps placed widgets to pixel rects given the orientation. Owns gutters/margins
and the page chrome (border, header, footer) that survive across layouts.
Single static layout for now: a fixed assignment of widgets to cells.

```go
// sketch
type Orientation int // Portrait (default) | Landscape
type Placement struct { Col, Row int; Widget Widget } // top-left cell
func Compose(o Orientation, placements []Placement) *image.Gray
```

### 3. Data layer (typed models + providers)

Each data domain has a typed model and a provider that produces it. This is the
**interface seam** that keeps integrations decoupled and renders testable
without a network.

```go
// sketch
type WeatherModel struct { /* current, hi/lo, next-N-hours … */ }

type WeatherProvider interface {
    Weather(ctx context.Context) (WeatherModel, error)
}
```

Implementations, in increasing privacy:
- `DemoWeather` — hardcoded values. **Build the widget against this first.**
- `OpenMeteoWeather` — wraps the existing `internal/weather` client (no key).
- `OpenWeatherMap` — API key via env (the key-based path).
- *(later)* `CalendarProvider`, `HomeAssistantProvider` — same interface
  pattern; keys/tokens + personal IDs via env.

**Inert-without-config rule:** a provider with no env config returns an error
(widget shows "unavailable") or is swapped for its `Demo*` sibling. A clone of
the public repo with no config does nothing private.

## Separation & safety

The decoupling that matters is the **interface seam** (widget ← typed model ←
provider). Integration code is public; **secrets and personal identifiers are
not**. Full rationale in
[D16](decisions.md#d16--widget-data-layer-in-repo-providers-secrets-via-env).

| Thing | Sensitive? | Where it lives |
| --- | --- | --- |
| API keys / tokens (OpenWeatherMap key, HA token, Google OAuth secret + refresh token) | **Yes — secret** | env var / mounted file on the host. Never in git, never in the image. |
| Personal identifiers (calendar IDs, HA entity IDs, HA URL, exact home coords) | Personal, not secret | untracked env / config file. Keeps the home's shape out of the public repo. |
| Integration logic | No | public code — fine. |

Safety practices (see also [server.md → Secret hygiene](server.md#secret-hygiene--config-public-repo)):
- Secrets via env / mounted file only; documented in `server.md` with
  **placeholder** values.
- The GHCR image is already secret-free (`FROM scratch`, just the binary);
  secrets enter at `docker run` time (`--env-file` on the host).
- Turn on GitHub **secret scanning + push protection** (free on public repos);
  optionally add a `gitleaks` CI step / pre-commit hook.
- Repo stays **public**; privacy is not the security mechanism. Going private is
  optional and only buys the convenience of committing *personal identifiers*.

## Next steps (M5 checklist)

Sequenced so each step ends with something runnable, and the two halves
(render vs data) are testable in isolation.

- [x] ~~**M5.0 — Widget seam refactor (no behaviour change).**~~ **Dropped** in
      favour of a redesign ([D17](decisions.md#d17--m5-widget-build-redesign-over-byte-for-byte-port-three-weather-cards-on-a-filling-22)).
      No PNG-parity golden; structural tests instead.
- [x] **M5.1 — 2×2 grid + orientation.** `internal/render/grid.go` does
      footprint→rect over filling cells with fixed header/footer bands;
      `?orientation=landscape` (portrait default). Rect math unit-tested
      (`grid_test.go`).
- [x] **M5.2 — Data layer + demo provider.** `internal/data` defines
      `WeatherModel` + `WeatherProvider`; `DemoWeather` is hardcoded and is the
      default provider during M5. Widgets render with zero network.
- [x] **M5.3 — Real weather provider behind the seam.** `data.OpenMeteo` adapter
      wraps the client+cache (`WEATHER_PROVIDER=openmeteo`, now the default). The
      Open-Meteo client fetches hourly precip probability/amount + a 3-day daily
      block (hi/lo, weather code, peak precip probability), so the rain card and
      3-day forecast render live data. `OpenWeatherMap` (env key) still optional.
- [x] **M5.4 — Composition proven.** Three widgets across the grid:
      `WeatherToday` (1×1), `WeatherForecast` (1×1), `Rain` (2×1). The `Rain`
      renderer is rect-agnostic — also rendable in the footer via `?rain=footer`.
      A non-weather widget (clock/date) is still open if wanted.
- [x] **M5.6 — Layout decisions server-side.** Rain defaults to the footer strip
      (`?rain=card` opts into the in-grid card); orientation is a server setting
      (`DASHBOARD_ORIENTATION`, `?orientation=` overrides). The device fetches a
      bare `/dashboard.png`; the preview drives only `batt`/`plug`.

**M5 complete** (2026-06-14). Carried forward to a later milestone:

- **First private source** (Calendar or Home Assistant) behind the same provider
  interface; keys/tokens + personal IDs via env, documented with placeholders in
  `server.md`. Add a `gitleaks` CI guard before it lands. *(Was M5.5.)*

## Open questions (deliberately deferred)

- Multi-layout selection mechanism (`?layout=`, time-of-day) — after widgets work.
- Which private source is first (Calendar vs Home Assistant) — at M5.5.
- Per-widget refresh hints / partial eink redraw — not now; the client fetches
  one full PNG per cycle.
