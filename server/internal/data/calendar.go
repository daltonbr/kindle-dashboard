package data

import (
	"context"
	"sort"
	"time"
)

// CalendarModel is the render-ready agenda the calendar widget draws from. It is
// source-agnostic: a CalendarProvider fills it from an iCal feed, a demo
// fixture, or any other source. Events are not assumed to be sorted; use
// Upcoming to get the chronological, future-facing slice the widget renders.
type CalendarModel struct {
	Events    []Event   // the events in the window the provider fetched
	FetchedAt time.Time // when the feed was retrieved
}

// Event is a single calendar entry, trimmed to what the agenda widget renders.
type Event struct {
	Title  string
	Start  time.Time
	End    time.Time
	AllDay bool // a date-only event (no meaningful time-of-day)
}

// Ended reports whether the event has finished as of t. An all-day event counts
// as ongoing through the end of its day.
func (e Event) Ended(t time.Time) bool {
	return e.End.Before(t) || e.End.Equal(t)
}

// Upcoming returns up to n events that have not yet ended as of from, sorted by
// start time ascending (ties broken by end time, then title for determinism).
// n <= 0 returns all matching events.
func (m CalendarModel) Upcoming(from time.Time, n int) []Event {
	out := make([]Event, 0, len(m.Events))
	for _, e := range m.Events {
		if !e.Ended(from) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Start.Equal(b.Start) {
			return a.Start.Before(b.Start)
		}
		if !a.End.Equal(b.End) {
			return a.End.Before(b.End)
		}
		return a.Title < b.Title
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// CalendarProvider produces a render-ready CalendarModel. Implementations must be
// inert without configuration: a provider with no feed URL configured returns an
// error so the widget renders an "unavailable" state, or is swapped for
// DemoCalendar (decision D16). The first real implementation reads a Google
// Calendar secret iCal URL (decision D19).
type CalendarProvider interface {
	Calendar(ctx context.Context) (CalendarModel, error)
}
