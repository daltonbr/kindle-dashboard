package data

import (
	"context"
	"testing"
	"time"
)

func TestCalendarModel_Upcoming_filtersAndSorts(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return time.Date(2026, 6, 14, h, 0, 0, 0, time.UTC) }

	m := CalendarModel{Events: []Event{
		{Title: "Later", Start: at(15), End: at(16)},
		{Title: "Over", Start: at(9), End: at(10)},     // ended before now → dropped
		{Title: "Ongoing", Start: at(11), End: at(13)}, // started, not ended → kept
		{Title: "Soon", Start: at(14), End: at(15)},
	}}

	got := m.Upcoming(now, 0)
	want := []string{"Ongoing", "Soon", "Later"}
	if len(got) != len(want) {
		t.Fatalf("Upcoming returned %d events, want %d: %+v", len(got), len(want), got)
	}
	for i, title := range want {
		if got[i].Title != title {
			t.Errorf("Upcoming[%d] = %q, want %q", i, got[i].Title, title)
		}
	}
}

func TestCalendarModel_Upcoming_limit(t *testing.T) {
	now := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return time.Date(2026, 6, 14, h, 0, 0, 0, time.UTC) }
	m := CalendarModel{Events: []Event{
		{Title: "A", Start: at(1), End: at(2)},
		{Title: "B", Start: at(3), End: at(4)},
		{Title: "C", Start: at(5), End: at(6)},
	}}

	if got := m.Upcoming(now, 2); len(got) != 2 {
		t.Fatalf("Upcoming(n=2) = %d events, want 2", len(got))
	}
	if got := m.Upcoming(now, 2); got[0].Title != "A" || got[1].Title != "B" {
		t.Errorf("Upcoming(n=2) = %q,%q, want A,B", got[0].Title, got[1].Title)
	}
}

func TestEvent_Ended(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 6, 14, h, 0, 0, 0, time.UTC) }
	e := Event{Start: at(10), End: at(11)}

	if e.Ended(at(10)) {
		t.Error("event should not be ended at its start")
	}
	if !e.Ended(at(11)) {
		t.Error("event should be ended at its end instant")
	}
	if !e.Ended(at(12)) {
		t.Error("event should be ended after its end")
	}
}

func TestDemoCalendar_provider(t *testing.T) {
	m, err := DemoCalendar{}.Calendar(context.Background())
	if err != nil {
		t.Fatalf("DemoCalendar returned error: %v", err)
	}
	if len(m.Events) == 0 {
		t.Fatal("DemoCalendar produced no events")
	}
}

func TestDemoCalendarModel_hasFutureEvents(t *testing.T) {
	ref := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	m := DemoCalendarModel(ref)

	up := m.Upcoming(ref, 5)
	if len(up) == 0 {
		t.Fatal("demo agenda has no upcoming events relative to its anchor")
	}
	// The 09:00 standup is before the noon anchor and must be filtered out.
	for _, e := range up {
		if e.Title == "Morning standup" {
			t.Error("expected the already-ended standup to be filtered from Upcoming")
		}
	}
}
