package calendar

import (
	"os"
	"testing"
	"time"
)

// loadSample parses the shared fixture for expansion tests.
func loadSample(t *testing.T) Calendar {
	t.Helper()
	raw, err := os.ReadFile("testdata/sample.ics")
	if err != nil {
		t.Fatal(err)
	}
	cal, err := Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return cal
}

// occAt reports whether an occurrence with the given summary starts at the
// given London wall time.
func occAt(occs []Occurrence, summary string, loc *time.Location, y int, m time.Month, d, hh, mm int) bool {
	want := time.Date(y, m, d, hh, mm, 0, 0, loc)
	for _, o := range occs {
		if o.Summary == summary && o.Start.Equal(want) {
			return true
		}
	}
	return false
}

func countSummary(occs []Occurrence, summary string) int {
	n := 0
	for _, o := range occs {
		if o.Summary == summary {
			n++
		}
	}
	return n
}

func TestExpand_sample(t *testing.T) {
	cal := loadSample(t)
	loc := london(t)

	from := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)
	occs := Expand(cal, from, 14*24*time.Hour) // window: 06-15 .. 06-29

	// One-off in window.
	if !occAt(occs, "Dentist appointment", loc, 2026, 6, 15, 14, 30) {
		t.Error("missing dentist occurrence")
	}

	// Weekly standup: Mon 06-15 present.
	if !occAt(occs, "Team standup", loc, 2026, 6, 15, 9, 0) {
		t.Error("missing Mon 06-15 standup")
	}
	// Wed 06-17 excluded by EXDATE.
	if occAt(occs, "Team standup", loc, 2026, 6, 17, 9, 0) {
		t.Error("EXDATE'd standup (06-17) should be absent")
	}
	// Fri 06-19 master instance suppressed by the override...
	if occAt(occs, "Team standup", loc, 2026, 6, 19, 9, 0) {
		t.Error("overridden master instance (06-19 09:00) should be suppressed")
	}
	// ...and the override appears at its moved time.
	if !occAt(occs, "Team standup (moved)", loc, 2026, 6, 19, 11, 0) {
		t.Error("override occurrence (06-19 11:00) should appear")
	}

	// All-day conference in window.
	if !occAt(occs, "Conference", loc, 2026, 6, 20, 0, 0) {
		t.Error("missing all-day conference")
	}

	// Cancelled event never appears.
	if countSummary(occs, "Cancelled thing") != 0 {
		t.Error("cancelled event leaked into agenda")
	}

	// UTC folded event in window.
	folded := "A very long summary that has been folded across multiple lines to test line unfolding behaviour"
	if countSummary(occs, folded) != 1 {
		t.Error("folded UTC event missing from window")
	}
}

func TestExpand_windowExcludesFarFuture(t *testing.T) {
	cal := loadSample(t)
	loc := london(t)
	from := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)

	// A 2-day window stops before the all-day conference on the 20th.
	occs := Expand(cal, from, 2*24*time.Hour)
	if countSummary(occs, "Conference") != 0 {
		t.Error("conference is outside a 2-day window but appeared")
	}
}

func TestExpand_keepsOngoing(t *testing.T) {
	loc := time.UTC
	cal := Calendar{DefaultLoc: loc, Events: []VEvent{{
		UID:     "x",
		Summary: "Long meeting",
		Start:   time.Date(2026, 6, 15, 8, 0, 0, 0, loc),
		End:     time.Date(2026, 6, 15, 18, 0, 0, 0, loc),
	}}}
	// "now" is mid-event; it should still surface (End is after from).
	from := time.Date(2026, 6, 15, 12, 0, 0, 0, loc)
	occs := Expand(cal, from, 24*time.Hour)
	if len(occs) != 1 {
		t.Fatalf("ongoing event not kept: %v", occs)
	}
}
