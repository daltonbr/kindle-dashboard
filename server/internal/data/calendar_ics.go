package data

import (
	"context"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/calendar"
)

// agendaSource is the slice of calendar.Cache this adapter needs. Declared as an
// interface so tests can substitute a fake without a live cache.
type agendaSource interface {
	Get(ctx context.Context) (calendar.Calendar, error)
}

// ICSCalendar adapts the internal/calendar client+cache to the CalendarProvider
// seam: it fetches the cached feed and expands it into concrete upcoming
// occurrences over a bounded horizon (decision D20), projected onto Events.
type ICSCalendar struct {
	src     agendaSource
	horizon time.Duration
	now     func() time.Time // injectable for tests
}

// DefaultHorizon is how far ahead recurring events are materialised. Generous
// enough to fill the agenda on a sparse calendar, bounded enough to keep
// expansion cheap.
const DefaultHorizon = 45 * 24 * time.Hour

// NewICSCalendar wraps a calendar.Cache as a CalendarProvider. A horizon <= 0
// uses DefaultHorizon.
func NewICSCalendar(cache *calendar.Cache, horizon time.Duration) *ICSCalendar {
	if horizon <= 0 {
		horizon = DefaultHorizon
	}
	return &ICSCalendar{src: cache, horizon: horizon, now: time.Now}
}

// Calendar fetches the feed and projects its upcoming occurrences onto the
// render model. Ordering and the final N cut are left to CalendarModel.Upcoming.
func (c *ICSCalendar) Calendar(ctx context.Context) (CalendarModel, error) {
	cal, err := c.src.Get(ctx)
	if err != nil {
		return CalendarModel{}, err
	}

	occs := calendar.Expand(cal, c.now(), c.horizon)
	events := make([]Event, len(occs))
	for i, o := range occs {
		events[i] = Event{
			Title:  o.Summary,
			Start:  o.Start,
			End:    o.End,
			AllDay: o.AllDay,
		}
	}

	return CalendarModel{Events: events, FetchedAt: cal.FetchedAt}, nil
}
