// Package weather is the Open-Meteo client + cache for the dashboard.
//
// M3.1 scope: just the typed client. TTL caching lands in M3.2.
package weather

import (
	"context"
	"encoding/json"
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
	Days      []DailyReading // chronological daily outlook; index 0 is today
	Next24h   []HourlyReading
	FetchedAt time.Time
}

// CurrentReading is the "right now" observation.
type CurrentReading struct {
	Time        time.Time
	TempC       float64
	WeatherCode int
}

// DailyReading summarises a single forecast day.
type DailyReading struct {
	Date         time.Time
	HighC, LowC  float64
	WeatherCode  int
	PrecipChance int // peak probability of precipitation over the day, 0–100%
}

// HourlyReading is one entry on the hourly curve.
type HourlyReading struct {
	Time         time.Time
	TempC        float64
	PrecipChance int     // probability of precipitation, 0–100%
	PrecipMM     float64 // expected precipitation amount, millimetres
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
	q.Set("daily", "temperature_2m_max,temperature_2m_min,weather_code,precipitation_probability_max")
	q.Set("hourly", "temperature_2m,precipitation_probability,precipitation")
	q.Set("timezone", "auto")
	q.Set("forecast_days", "3")
	return c.baseURL + "/v1/forecast?" + q.Encode()
}

// openMeteoResponse mirrors the JSON shape we care about.
//
// Timestamps come back as local-time strings without offset (e.g. "2026-05-25T17:00")
// because we request timezone=auto. We combine them with utc_offset_seconds to
// produce real time.Time values that compare correctly to time.Now().
type openMeteoResponse struct {
	UTCOffsetSeconds int `json:"utc_offset_seconds"`
	Current          struct {
		Time        string  `json:"time"`
		Temperature float64 `json:"temperature_2m"`
		WeatherCode int     `json:"weather_code"`
	} `json:"current"`
	Daily struct {
		Time          []string  `json:"time"`
		Max           []float64 `json:"temperature_2m_max"`
		Min           []float64 `json:"temperature_2m_min"`
		WeatherCode   []int     `json:"weather_code"`
		PrecipProbMax []int     `json:"precipitation_probability_max"`
	} `json:"daily"`
	Hourly struct {
		Time          []string  `json:"time"`
		Temperature   []float64 `json:"temperature_2m"`
		PrecipChance  []int     `json:"precipitation_probability"`
		Precipitation []float64 `json:"precipitation"`
	} `json:"hourly"`
}

const (
	apiTimeLayout = "2006-01-02T15:04"
	apiDateLayout = "2006-01-02"
)

func (r openMeteoResponse) toForecast() (Forecast, error) {
	loc := time.FixedZone("api", r.UTCOffsetSeconds)

	currentTime, err := time.ParseInLocation(apiTimeLayout, r.Current.Time, loc)
	if err != nil {
		return Forecast{}, fmt.Errorf("parse current.time %q: %w", r.Current.Time, err)
	}

	if len(r.Hourly.Time) != len(r.Hourly.Temperature) {
		return Forecast{}, fmt.Errorf("open-meteo: hourly time/temperature length mismatch (%d vs %d)",
			len(r.Hourly.Time), len(r.Hourly.Temperature))
	}

	hourly, err := buildHourly(r.Hourly.Time, r.Hourly.Temperature, r.Hourly.PrecipChance, r.Hourly.Precipitation, loc)
	if err != nil {
		return Forecast{}, err
	}

	days, err := buildDaily(r.Daily.Time, r.Daily.Max, r.Daily.Min, r.Daily.WeatherCode, r.Daily.PrecipProbMax, loc)
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
		Days:      days,
		Next24h:   next24,
		FetchedAt: time.Now().UTC(),
	}, nil
}

func buildHourly(times []string, temps []float64, probs []int, mm []float64, loc *time.Location) ([]HourlyReading, error) {
	out := make([]HourlyReading, 0, len(times))
	for i, ts := range times {
		t, err := time.ParseInLocation(apiTimeLayout, ts, loc)
		if err != nil {
			return nil, fmt.Errorf("parse hourly time %q: %w", ts, err)
		}
		hr := HourlyReading{Time: t, TempC: temps[i]}
		if i < len(probs) {
			hr.PrecipChance = probs[i]
		}
		if i < len(mm) {
			hr.PrecipMM = mm[i]
		}
		out = append(out, hr)
	}
	return out, nil
}

// buildDaily maps the daily arrays into chronological DailyReadings. Max/Min are
// required; weather code and precip probability are filled when present (they
// align index-for-index with the date series).
func buildDaily(times []string, max, min []float64, codes, precipProb []int, loc *time.Location) ([]DailyReading, error) {
	n := len(times)
	if n == 0 || len(max) < n || len(min) < n {
		return nil, fmt.Errorf("open-meteo: empty or short daily section (time=%d max=%d min=%d)",
			n, len(max), len(min))
	}
	out := make([]DailyReading, 0, n)
	for i := range n {
		d, err := time.ParseInLocation(apiDateLayout, times[i], loc)
		if err != nil {
			return nil, fmt.Errorf("parse daily date %q: %w", times[i], err)
		}
		dr := DailyReading{Date: d, HighC: max[i], LowC: min[i]}
		if i < len(codes) {
			dr.WeatherCode = codes[i]
		}
		if i < len(precipProb) {
			dr.PrecipChance = precipProb[i]
		}
		out = append(out, dr)
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
