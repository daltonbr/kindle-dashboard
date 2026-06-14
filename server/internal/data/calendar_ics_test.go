package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/calendar"
)

type fakeAgenda struct {
	cal calendar.Calendar
	err error
}

func (f fakeAgenda) Get(_ context.Context) (calendar.Calendar, error) {
	return f.cal, f.err
}

func TestICSCalendar_projectsAndExpands(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)
	fetched := time.Date(2026, 6, 14, 23, 0, 0, 0, loc)

	src := fakeAgenda{cal: calendar.Calendar{
		DefaultLoc: loc,
		FetchedAt:  fetched,
		Events: []calendar.VEvent{{
			UID:     "x",
			Summary: "Dentist",
			Start:   time.Date(2026, 6, 16, 10, 0, 0, 0, loc),
			End:     time.Date(2026, 6, 16, 11, 0, 0, 0, loc),
		}},
	}}

	c := &ICSCalendar{src: src, horizon: DefaultHorizon, now: func() time.Time { return now }}
	m, err := c.Calendar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !m.FetchedAt.Equal(fetched) {
		t.Errorf("FetchedAt = %v, want %v", m.FetchedAt, fetched)
	}
	if len(m.Events) != 1 {
		t.Fatalf("projected %d events, want 1", len(m.Events))
	}
	if m.Events[0].Title != "Dentist" {
		t.Errorf("title = %q", m.Events[0].Title)
	}
	// The projected model flows through the data-layer helper unchanged.
	if up := m.Upcoming(now, 5); len(up) != 1 {
		t.Errorf("Upcoming = %d, want 1", len(up))
	}
}

func TestICSCalendar_propagatesError(t *testing.T) {
	c := &ICSCalendar{src: fakeAgenda{err: errors.New("feed down")}, horizon: DefaultHorizon, now: time.Now}
	if _, err := c.Calendar(context.Background()); err == nil {
		t.Error("expected error to propagate from the source")
	}
}

func TestNewICSCalendar_defaultsHorizon(t *testing.T) {
	c := NewICSCalendar(nil, 0)
	if c.horizon != DefaultHorizon {
		t.Errorf("horizon = %v, want DefaultHorizon", c.horizon)
	}
}
