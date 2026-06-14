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
	logger := newLogger(envOrDefault("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	provider, err := buildWeatherProvider()
	if err != nil {
		slog.Error("weather config", "err", err)
		os.Exit(1)
	}

	orientationName := envOrDefault("DASHBOARD_ORIENTATION", "portrait")
	defaultOrientation := orientationFromName(orientationName)
	slog.Info("default orientation", "value", orientationName)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /dashboard.png", makeDashboardHandler(provider, defaultOrientation))
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

func makeDashboardHandler(provider data.WeatherProvider, defaultOrientation render.Orientation) http.HandlerFunc {
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

		q := r.URL.Query()
		opts := render.Options{
			Orientation:  parseOrientation(q, defaultOrientation),
			Now:          time.Now(),
			Battery:      parseBattery(q),
			RainInFooter: parseRainInFooter(q),
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
