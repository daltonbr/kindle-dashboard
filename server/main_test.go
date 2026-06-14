package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthcheck(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okSrv.Close()

	downSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer downSrv.Close()

	cases := []struct {
		name string
		url  string
		want int
	}{
		{"healthy 200", okSrv.URL, 0},
		{"unhealthy 503", downSrv.URL, 1},
		{"connection refused", "http://127.0.0.1:0/healthz", 1},
		{"bad url", "http://%zz", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := healthcheck(c.url, time.Second); got != c.want {
				t.Errorf("healthcheck(%q) = %d, want %d", c.url, got, c.want)
			}
		})
	}
}
