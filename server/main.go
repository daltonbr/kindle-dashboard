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
	"syscall"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/render"
	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

//go:embed preview.html
var previewHTML []byte

const (
	panelWidth  = 600
	panelHeight = 800

	// Per-request budget for fetching weather. Cron-side curl times out at 20s,
	// PNG encode is ~10ms — 8s gives Open-Meteo + cache plenty of headroom and
	// still leaves time for the encode + transfer.
	weatherFetchTimeout = 8 * time.Second
)

func main() {
	port := envOrDefault("PORT", "8080")
	logger := newLogger(envOrDefault("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	cache, err := buildWeatherCache()
	if err != nil {
		slog.Error("weather config", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /dashboard.png", makeDashboardHandler(cache))
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

func makeDashboardHandler(cache *weather.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fetchCtx, cancel := context.WithTimeout(r.Context(), weatherFetchTimeout)
		defer cancel()

		var forecast *weather.Forecast
		fc, err := cache.Get(fetchCtx)
		if err != nil {
			slog.Warn("weather fetch failed", "err", err)
		} else {
			forecast = &fc
		}

		img := render.Dashboard(panelWidth, panelHeight, time.Now(), forecast)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		if err := png.Encode(w, img); err != nil {
			slog.Error("png encode", "err", err)
		}
	}
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
