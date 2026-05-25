// Package weather is the Open-Meteo client + cache for the dashboard.
//
// M3.1 scope: just the typed client. TTL caching lands in M3.2.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultBaseURL = "https://api.open-meteo.com"

// Forecast is the subset of the Open-Meteo response we actually render.
type Forecast struct {
	Now       CurrentReading
	HighToday float64
	LowToday  float64
	Next24h   []HourlyReading
	FetchedAt time.Time
}

// CurrentReading is the "right now" observation.
type CurrentReading struct {
	Time        time.Time
	TempC       float64
	WeatherCode int
}

// HourlyReading is one entry on the hourly temperature curve.
type HourlyReading struct {
	Time  time.Time
	TempC float64
}

// Client fetches forecasts from Open-Meteo.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a Client. Pass empty baseURL to use the public Open-Meteo endpoint.
// Pass nil httpClient to get a sensible default with a 10s timeout.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: baseURL, http: httpClient}
}

// Fetch retrieves a forecast for the given coordinates.
func (c *Client) Fetch(ctx context.Context, lat, lon float64) (Forecast, error) {
	u := c.buildURL(lat, lon)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Forecast{}, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Forecast{}, fmt.Errorf("fetch open-meteo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Forecast{}, fmt.Errorf("open-meteo status %d: %s", resp.StatusCode, string(body))
	}

	var raw openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Forecast{}, fmt.Errorf("decode open-meteo: %w", err)
	}

	return raw.toForecast()
}

func (c *Client) buildURL(lat, lon float64) string {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', -1, 64))
	q.Set("current", "temperature_2m,weather_code")
	q.Set("daily", "temperature_2m_max,temperature_2m_min")
	q.Set("hourly", "temperature_2m")
	q.Set("timezone", "auto")
	q.Set("forecast_days", "2")
	return c.baseURL + "/v1/forecast?" + q.Encode()
}

// openMeteoResponse mirrors the JSON shape we care about.
// Timestamps come back as local-time strings without offset (e.g. "2026-05-25T17:00")
// because we request timezone=auto. We parse them as naive UTC purely so they're
// mutually comparable — the absolute zone doesn't matter for slicing.
type openMeteoResponse struct {
	Current struct {
		Time        string  `json:"time"`
		Temperature float64 `json:"temperature_2m"`
		WeatherCode int     `json:"weather_code"`
	} `json:"current"`
	Daily struct {
		Time []string  `json:"time"`
		Max  []float64 `json:"temperature_2m_max"`
		Min  []float64 `json:"temperature_2m_min"`
	} `json:"daily"`
	Hourly struct {
		Time        []string  `json:"time"`
		Temperature []float64 `json:"temperature_2m"`
	} `json:"hourly"`
}

const apiTimeLayout = "2006-01-02T15:04"

func (r openMeteoResponse) toForecast() (Forecast, error) {
	currentTime, err := time.Parse(apiTimeLayout, r.Current.Time)
	if err != nil {
		return Forecast{}, fmt.Errorf("parse current.time %q: %w", r.Current.Time, err)
	}

	if len(r.Daily.Max) == 0 || len(r.Daily.Min) == 0 {
		return Forecast{}, errors.New("open-meteo: empty daily section")
	}

	if len(r.Hourly.Time) != len(r.Hourly.Temperature) {
		return Forecast{}, fmt.Errorf("open-meteo: hourly time/temperature length mismatch (%d vs %d)",
			len(r.Hourly.Time), len(r.Hourly.Temperature))
	}

	hourly, err := buildHourly(r.Hourly.Time, r.Hourly.Temperature)
	if err != nil {
		return Forecast{}, err
	}

	next24 := sliceNext24(hourly, currentTime)

	return Forecast{
		Now: CurrentReading{
			Time:        currentTime,
			TempC:       r.Current.Temperature,
			WeatherCode: r.Current.WeatherCode,
		},
		HighToday: r.Daily.Max[0],
		LowToday:  r.Daily.Min[0],
		Next24h:   next24,
		FetchedAt: time.Now().UTC(),
	}, nil
}

func buildHourly(times []string, temps []float64) ([]HourlyReading, error) {
	out := make([]HourlyReading, 0, len(times))
	for i, ts := range times {
		t, err := time.Parse(apiTimeLayout, ts)
		if err != nil {
			return nil, fmt.Errorf("parse hourly time %q: %w", ts, err)
		}
		out = append(out, HourlyReading{Time: t, TempC: temps[i]})
	}
	return out, nil
}

// sliceNext24 returns up to 24 hourly readings starting from the first entry
// at or after the current observation. If fewer than 24 remain (end of the
// forecast window), returns what's available.
func sliceNext24(hourly []HourlyReading, now time.Time) []HourlyReading {
	start := -1
	for i, h := range hourly {
		if !h.Time.Before(now) {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	end := min(start+24, len(hourly))
	return hourly[start:end]
}
