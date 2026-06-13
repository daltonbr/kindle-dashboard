# Widget architecture (M5) — design + plan

Status: **planning / not yet built.** This is the handoff doc for the M5 work.
Read it cold, then jump to the [step checklist](#next-steps-m5-checklist).

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
| Grid | **2×2 quadrants + spans.** Footprints: 1×1 (quarter), 2×1 / 1×2 (half), 2×2 (full). |
| Orientation | **Portrait default (600×800)**; landscape (800×600) supported as a render parameter. Grid is always 2 cols × 2 rows; cell pixel size derives from orientation (portrait cell 300×400, landscape cell 400×300). |
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

- [ ] **M5.0 — Widget seam refactor (no behaviour change).** Extract a `Widget`
      interface from the current `panels.Weather`. Keep today's single
      full-screen weather layout, but route it through the new grid/`Compose`
      path. A golden/snapshot test asserts the PNG is byte-for-byte unchanged.
- [ ] **M5.1 — 2×2 grid + orientation.** Implement footprint→rect mapping for
      portrait (default) and landscape. Unit-test the rect math for each
      footprint in both orientations. Add `?orientation=landscape` to the
      handler (portrait default).
- [ ] **M5.2 — Data layer + demo provider.** Define `WeatherModel` +
      `WeatherProvider`; add `DemoWeather` (hardcoded). Re-point the weather
      widget at the provider. Widget now renders with zero network — the
      "develop the halves separately" milestone.
- [ ] **M5.3 — Real weather provider behind the seam.** Wrap existing
      Open-Meteo as `OpenMeteoWeather`; optionally add `OpenWeatherMap`
      (key via env). Provider selected by config; falls back to demo/unavailable
      when unconfigured. No widget/grid changes.
- [ ] **M5.4 — Second widget (proves composition).** Add one more widget in a
      different cell (candidate: a clock/date widget, or a placeholder) so the
      2×2 grid is exercised with >1 widget.
- [ ] **M5.5 — (deferred) First private source.** Calendar or Home Assistant,
      behind the same provider interface; keys/tokens + personal IDs via env,
      documented with placeholders in `server.md`. Add a `gitleaks` CI guard
      before this lands.

## Open questions (deliberately deferred)

- Multi-layout selection mechanism (`?layout=`, time-of-day) — after widgets work.
- Which private source is first (Calendar vs Home Assistant) — at M5.5.
- Per-widget refresh hints / partial eink redraw — not now; the client fetches
  one full PNG per cycle.
