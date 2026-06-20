package main

import (
	"context"
	_ "embed"
	"errors"
	"image/png"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/calendar"
	"github.com/daltonbr/kindle-dashboard/server/internal/data"
	"github.com/daltonbr/kindle-dashboard/server/internal/render"
	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

//go:embed preview.html
var previewHTML []byte

// Per-request budget for fetching weather. Cron-side curl times out at 20s,
// PNG encode is ~10ms — 8s gives Open-Meteo + cache plenty of headroom and
// still leaves time for the encode + transfer.
const weatherFetchTimeout = 8 * time.Second

func main() {
	port := envOrDefault("PORT", "8080")

	// Healthcheck subcommand: the FROM-scratch image has no shell, so a Docker
	// HEALTHCHECK can't use wget/curl. `server healthcheck` probes /healthz on
	// localhost and exits 0 (healthy) / 1 (not), giving compose a self-contained
	// check: HEALTHCHECK CMD ["/server", "healthcheck"].
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck("http://127.0.0.1:"+port+"/healthz", 2*time.Second))
	}

	logger := newLogger(envOrDefault("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	provider, err := buildWeatherProvider()
	if err != nil {
		slog.Error("weather config", "err", err)
		os.Exit(1)
	}

	calProvider, err := buildCalendarProvider()
	if err != nil {
		slog.Error("calendar config", "err", err)
		os.Exit(1)
	}

	orientationName := envOrDefault("DASHBOARD_ORIENTATION", "portrait")
	defaultOrientation := orientationFromName(orientationName)
	slog.Info("default orientation", "value", orientationName)

	loc, err := buildLocation()
	if err != nil {
		slog.Error("timezone config", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /dashboard.png", makeDashboardHandler(provider, calProvider, defaultOrientation, loc))
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /preview", handlePreview)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/preview", http.StatusFound)
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      logRequests(mux),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

// buildWeatherProvider selects the weather data source. Defaults to the live
// Open-Meteo client+cache; set WEATHER_PROVIDER=demo for the network-free
// fixture (useful for widget development and offline runs).
func buildWeatherProvider() (data.WeatherProvider, error) {
	switch strings.ToLower(envOrDefault("WEATHER_PROVIDER", "openmeteo")) {
	case "openmeteo":
		cache, err := buildWeatherCache()
		if err != nil {
			return nil, err
		}
		slog.Info("weather provider", "kind", "openmeteo")
		return data.NewOpenMeteo(cache), nil
	default:
		slog.Info("weather provider", "kind", "demo")
		return data.DemoWeather{}, nil
	}
}

// buildCalendarProvider selects the agenda data source. It is inert by default:
// with neither CALENDAR_ICS_URL nor CALENDAR_PROVIDER set it returns nil, so a
// clone with no config simply shows no agenda card (decision D16/D19). Set
// CALENDAR_PROVIDER=demo for the network-free fixture, or CALENDAR_ICS_URL to a
// Google Calendar secret iCal feed for live data (cached for CALENDAR_TTL,
// default 15m).
func buildCalendarProvider() (data.CalendarProvider, error) {
	if strings.EqualFold(os.Getenv("CALENDAR_PROVIDER"), "demo") {
		slog.Info("calendar provider", "kind", "demo")
		return data.DemoCalendar{}, nil
	}

	url := os.Getenv("CALENDAR_ICS_URL")
	if url == "" {
		slog.Info("calendar provider", "kind", "none (CALENDAR_ICS_URL unset)")
		return nil, nil
	}

	ttl, err := time.ParseDuration(envOrDefault("CALENDAR_TTL", "15m"))
	if err != nil {
		return nil, errors.New("CALENDAR_TTL: " + err.Error())
	}
	// Log only the TTL — never the URL; it is a secret credential (D19).
	slog.Info("calendar provider", "kind", "ics", "ttl", ttl)
	cache := calendar.NewCache(calendar.NewClient(url, nil), ttl)
	return data.NewICSCalendar(cache, 0), nil
}

func buildWeatherCache() (*weather.Cache, error) {
	lat, err := strconv.ParseFloat(envOrDefault("WEATHER_LAT", "50.8225"), 64)
	if err != nil {
		return nil, errors.New("WEATHER_LAT: " + err.Error())
	}
	lon, err := strconv.ParseFloat(envOrDefault("WEATHER_LON", "-0.1372"), 64)
	if err != nil {
		return nil, errors.New("WEATHER_LON: " + err.Error())
	}
	ttl, err := time.ParseDuration(envOrDefault("WEATHER_TTL", "10m"))
	if err != nil {
		return nil, errors.New("WEATHER_TTL: " + err.Error())
	}
	slog.Info("weather configured", "lat", lat, "lon", lon, "ttl", ttl)
	return weather.NewCache(weather.NewClient("", nil), lat, lon, ttl), nil
}

// buildLocation resolves the display time zone used to stamp render times.
// Everything time-of-day on the panel — agenda event times, the header date,
// the "updated HH:MM" line, the month grid — is rendered in this location, so
// it must be the wall's real local zone, not the container's. The production
// image is FROM scratch with time.Local == UTC, which would otherwise render
// every clock in GMT (an hour behind during BST). Defaults to Europe/London to
// match the hard-coded Brighton weather coordinates; using an IANA zone (not a
// fixed offset) means the BST↔GMT switch is handled automatically. Resolves
// against the embedded zoneinfo (time/tzdata, imported by the calendar pkg).
func buildLocation() (*time.Location, error) {
	name := envOrDefault("DASHBOARD_TIMEZONE", "Europe/London")
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, errors.New("DASHBOARD_TIMEZONE: " + err.Error())
	}
	slog.Info("display timezone", "value", name)
	return loc, nil
}

func makeDashboardHandler(provider data.WeatherProvider, calProvider data.CalendarProvider, defaultOrientation render.Orientation, loc *time.Location) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fetchCtx, cancel := context.WithTimeout(r.Context(), weatherFetchTimeout)
		defer cancel()

		var model *data.WeatherModel
		m, err := provider.Weather(fetchCtx)
		if err != nil {
			slog.Warn("weather fetch failed", "err", err)
		} else {
			model = &m
		}

		// Calendar is independent of weather: a failure here just omits the
		// agenda card, it doesn't blank the dashboard.
		var calModel *data.CalendarModel
		if calProvider != nil {
			if cm, err := calProvider.Calendar(fetchCtx); err != nil {
				slog.Warn("calendar fetch failed", "err", err)
			} else {
				calModel = &cm
			}
		}

		q := r.URL.Query()
		opts := render.Options{
			Orientation:  parseOrientation(q, defaultOrientation),
			Now:          time.Now().In(loc),
			Battery:      parseBattery(q),
			RainInFooter: parseRainInFooter(q),
			Calendar:     calModel,
		}

		img := render.Dashboard(model, opts)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		if err := png.Encode(w, img); err != nil {
			slog.Error("png encode", "err", err)
		}
	}
}

// parseOrientation reads ?orientation=portrait|landscape, falling back to the
// server-configured default when the param is absent or unrecognised. The wall
// device relies on the default; the param exists mainly for the preview page.
func parseOrientation(q map[string][]string, def render.Orientation) render.Orientation {
	v := q["orientation"]
	if len(v) == 0 || v[0] == "" {
		return def
	}
	switch {
	case strings.EqualFold(v[0], "landscape"):
		return render.Landscape
	case strings.EqualFold(v[0], "portrait"):
		return render.Portrait
	default:
		return def
	}
}

// orientationFromName maps the DASHBOARD_ORIENTATION setting to a render value.
// Anything other than "landscape" is portrait (the default wall layout).
func orientationFromName(s string) render.Orientation {
	if strings.EqualFold(s, "landscape") {
		return render.Landscape
	}
	return render.Portrait
}

// parseRainInFooter decides where the rain timeline is drawn. Footer is the
// server-side default so the wall device can fetch a bare /dashboard.png; pass
// ?rain=card to place it as the in-grid 2×1 card instead.
func parseRainInFooter(q map[string][]string) bool {
	if v := q["rain"]; len(v) > 0 && strings.EqualFold(v[0], "card") {
		return false
	}
	return true
}

// parseBattery extracts the optional battery widget params from the
// query string. Returns nil when `batt` is absent or malformed, so the
// renderer skips the widget entirely.
//
//	?batt=53        → Battery{Level: 53, Charging: false}
//	?batt=53&plug=1 → Battery{Level: 53, Charging: true}
//
// `plug` accepts 1/0, true/false (case-insensitive); anything else is false.
func parseBattery(q map[string][]string) *render.Battery {
	raw, ok := q["batt"]
	if !ok || len(raw) == 0 || raw[0] == "" {
		return nil
	}
	level, err := strconv.Atoi(raw[0])
	if err != nil {
		return nil
	}
	charging := false
	if p := q["plug"]; len(p) > 0 {
		switch strings.ToLower(p[0]) {
		case "1", "true", "yes", "on":
			charging = true
		}
	}
	return &render.Battery{Level: level, Charging: charging}
}

// healthcheck performs a one-shot GET against url and returns a process exit
// code: 0 if the server answers 200, 1 otherwise (connection refused, timeout,
// or any non-200). Used by the `server healthcheck` subcommand.
func healthcheck(url string, timeout time.Duration) int {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		return 0
	}
	return 1
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func handlePreview(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(previewHTML)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("req",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"dur", time.Since(start).String(),
		)
	})
}

func envOrDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
