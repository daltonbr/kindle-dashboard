package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func london(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("LoadLocation(Europe/London): %v — is time/tzdata embedded?", err)
	}
	return loc
}

func findEvent(cal Calendar, uidSummary string) (VEvent, bool) {
	for _, e := range cal.Events {
		if e.UID+"/"+e.Summary == uidSummary {
			return e, true
		}
	}
	return VEvent{}, false
}

func TestParse_sampleFeed(t *testing.T) {
	raw, err := os.ReadFile("testdata/sample.ics")
	if err != nil {
		t.Fatal(err)
	}
	cal, err := Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}

	if cal.DefaultLoc.String() != "Europe/London" {
		t.Errorf("DefaultLoc = %q, want Europe/London", cal.DefaultLoc)
	}
	if len(cal.Events) != 6 {
		t.Fatalf("parsed %d events, want 6", len(cal.Events))
	}

	loc := london(t)

	// Timed event with TZID.
	e, ok := findEvent(cal, "single-1/Dentist appointment")
	if !ok {
		t.Fatal("missing single event")
	}
	want := time.Date(2026, 6, 15, 14, 30, 0, 0, loc)
	if !e.Start.Equal(want) {
		t.Errorf("Dentist start = %v, want %v", e.Start, want)
	}
	if e.AllDay {
		t.Error("Dentist should not be all-day")
	}

	// Recurring master keeps its RRULE + EXDATE.
	master, ok := findEvent(cal, "weekly-1/Team standup")
	if !ok {
		t.Fatal("missing weekly master")
	}
	if master.RRule == nil {
		t.Fatal("weekly master lost its RRULE")
	}
	if len(master.ExDates) != 1 {
		t.Errorf("weekly master EXDATE count = %d, want 1", len(master.ExDates))
	}

	// Override carries a RECURRENCE-ID.
	ov, ok := findEvent(cal, "weekly-1/Team standup (moved)")
	if !ok {
		t.Fatal("missing override")
	}
	if ov.RecurrenceID.IsZero() {
		t.Error("override missing RECURRENCE-ID")
	}

	// All-day via VALUE=DATE.
	ad, ok := findEvent(cal, "allday-1/Conference")
	if !ok {
		t.Fatal("missing all-day event")
	}
	if !ad.AllDay {
		t.Error("Conference should be all-day")
	}
	if !ad.End.Equal(time.Date(2026, 6, 22, 0, 0, 0, 0, loc)) {
		t.Errorf("Conference end = %v, want 2026-06-22 midnight", ad.End)
	}

	// Cancelled flag.
	if c, ok := findEvent(cal, "cancelled-1/Cancelled thing"); !ok || !c.Cancelled {
		t.Error("cancelled event not flagged")
	}

	// Line unfolding + UTC (Z) parsing.
	f, ok := findEvent(cal, "folded-1/A very long summary that has been folded across multiple lines to test line unfolding behaviour")
	if !ok {
		t.Fatal("line unfolding failed (summary not joined)")
	}
	if !f.Start.Equal(time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("UTC start = %v, want 12:00Z", f.Start.UTC())
	}
}

func TestUnescapeText(t *testing.T) {
	got := unescapeText(`Lunch\, then gym\; bring kit\\notes`)
	want := "Lunch, then gym; bring kit\\notes"
	if got != want {
		t.Errorf("unescapeText = %q, want %q", got, want)
	}
}

func TestParse_missingDTENDDefaults(t *testing.T) {
	loc := london(t)
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:x\r\nSUMMARY:No end\r\n" +
		"DTSTART;TZID=Europe/London:20260615T090000\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	cal, err := Parse(ics)
	if err != nil {
		t.Fatal(err)
	}
	if len(cal.Events) != 1 {
		t.Fatalf("got %d events", len(cal.Events))
	}
	e := cal.Events[0]
	if !e.End.Equal(time.Date(2026, 6, 15, 9, 0, 0, 0, loc)) {
		t.Errorf("timed event with no DTEND should default to zero-length, got end %v", e.End)
	}
}

func TestClient_Fetch(t *testing.T) {
	raw, _ := os.ReadFile("testdata/sample.ics")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	fixed := time.Date(2026, 6, 14, 8, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return fixed }

	cal, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cal.Events) != 6 {
		t.Errorf("fetched %d events, want 6", len(cal.Events))
	}
	if !cal.FetchedAt.Equal(fixed) {
		t.Errorf("FetchedAt = %v, want %v", cal.FetchedAt, fixed)
	}
}

func TestClient_Fetch_non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL, srv.Client()).Fetch(context.Background()); err == nil {
		t.Error("expected error on 403, got nil")
	}
}
