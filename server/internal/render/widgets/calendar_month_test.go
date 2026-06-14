package widgets

import (
	"image"
	"testing"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

func monthFixture(now time.Time) data.CalendarModel {
	mk := func(title string, daysOut int) data.Event {
		s := now.Add(time.Duration(daysOut)*24*time.Hour + time.Hour)
		return data.Event{Title: title, Start: s, End: s.Add(time.Hour)}
	}
	return data.CalendarModel{
		FetchedAt: now,
		Events: []data.Event{
			mk("A", 0), mk("B", 1), mk("C", 2), // agenda's window (cap 3)
			mk("D", 6), mk("E", 12), mk("F", 20), // the "later" tail
		},
	}
}

func TestCalendarMonth_laterEvents(t *testing.T) {
	now := time.Date(2026, 6, 14, 11, 0, 0, 0, time.UTC)
	w := CalendarMonth{M: monthFixture(now), Now: now}

	later := w.laterEvents(now)
	if len(later) != 2 {
		t.Fatalf("got %d later events, want 2 (cap beyond the agenda's 3)", len(later))
	}
	if later[0].Title != "D" || later[1].Title != "E" {
		t.Errorf("later titles = %q,%q, want D,E (events 4-5, skipping the agenda's 3)",
			later[0].Title, later[1].Title)
	}
}

func TestCalendarMonth_laterEventsNoneWhenAgendaCoversAll(t *testing.T) {
	now := time.Date(2026, 6, 14, 11, 0, 0, 0, time.UTC)
	m := data.CalendarModel{FetchedAt: now, Events: []data.Event{
		{Title: "Only", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour)},
	}}
	if later := (CalendarMonth{M: m, Now: now}).laterEvents(now); later != nil {
		t.Errorf("got %v, want nil (one event fits in the agenda, nothing left over)", later)
	}
}

func TestCalendarMonth_eventDaySet(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 14, 11, 0, 0, 0, loc)
	w := CalendarMonth{M: monthFixture(now), Now: now}
	days := w.eventDaySet(loc)

	// Two events fall on the same calendar day → one key; distinct days → distinct keys.
	if !days[time.Date(2026, 6, 14, 0, 0, 0, 0, loc)] {
		t.Error("today (event A) not marked")
	}
	if !days[time.Date(2026, 6, 20, 0, 0, 0, 0, loc)] {
		t.Error("event D's day (Jun 20) not marked")
	}
	if days[time.Date(2026, 6, 15, 0, 0, 0, 0, loc)] && !hasEventOn(w, 2026, 6, 15) {
		t.Error("Jun 15 marked but no event starts then")
	}
}

func hasEventOn(w CalendarMonth, y, m, d int) bool {
	for _, e := range w.M.Events {
		ey, em, ed := e.Start.Date()
		if int(ey) == y && int(em) == m && ed == d {
			return true
		}
	}
	return false
}

func TestCalendarMonth_rendersInk(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 300, 320))
	fillWhite(img)
	area := image.Rect(12, 12, 288, 308)

	now := time.Date(2026, 6, 14, 11, 0, 0, 0, time.UTC)
	CalendarMonth{M: monthFixture(now), Now: now}.Render(img, area)
	if !hasInk(img, area) {
		t.Error("month grid drew no ink")
	}

	// Empty calendar still draws the grid (no panic, ink present, no footer).
	img2 := image.NewGray(image.Rect(0, 0, 300, 320))
	fillWhite(img2)
	CalendarMonth{M: data.CalendarModel{FetchedAt: now}, Now: now}.Render(img2, area)
	if !hasInk(img2, area) {
		t.Error("empty month grid drew no ink (expected the day grid)")
	}
}
