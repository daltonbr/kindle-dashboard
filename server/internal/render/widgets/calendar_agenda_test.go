package widgets

import (
	"image"
	"testing"
	"time"

	"golang.org/x/image/font"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

func TestDayLabel(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, loc) // Monday

	cases := []struct {
		name  string
		start time.Time
		want  string
	}{
		{"earlier today", time.Date(2026, 6, 15, 14, 0, 0, 0, loc), "Today"},
		{"tomorrow", time.Date(2026, 6, 16, 9, 0, 0, 0, loc), "Tomorrow"},
		{"this week", time.Date(2026, 6, 18, 9, 0, 0, 0, loc), "Thu"},
		{"far out", time.Date(2026, 7, 1, 9, 0, 0, 0, loc), "1 Jul"},
	}
	for _, c := range cases {
		if got := dayLabel(now, c.start); got != c.want {
			t.Errorf("%s: dayLabel = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWhenLabel(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, loc)

	timed := data.Event{Title: "Dentist", Start: time.Date(2026, 6, 15, 14, 30, 0, 0, loc)}
	if got := whenLabel(now, timed); got != "Today · 14:30" {
		t.Errorf("timed whenLabel = %q", got)
	}

	allDay := data.Event{Title: "Conference", Start: time.Date(2026, 6, 16, 0, 0, 0, 0, loc), AllDay: true}
	if got := whenLabel(now, allDay); got != "Tomorrow · all day" {
		t.Errorf("all-day whenLabel = %q", got)
	}
}

func TestTruncateToWidth(t *testing.T) {
	f := face(30)
	short := "Gym"
	if got := truncateToWidth(f, short, 1000); got != short {
		t.Errorf("short string was altered: %q", got)
	}

	long := "A really very extraordinarily long event title that will not fit"
	got := truncateToWidth(f, long, 120)
	if got == long {
		t.Error("long string was not truncated")
	}
	if measure := font.MeasureString(f, got).Round(); measure > 120 {
		t.Errorf("truncated string still %dpx wide, want <= 120", measure)
	}
}

func TestCalendarAgenda_emptyState(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 300, 320))
	fillWhite(img)
	area := image.Rect(12, 12, 288, 308)

	// A model whose only event is already over → nothing upcoming.
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	past := data.CalendarModel{
		FetchedAt: now,
		Events: []data.Event{{
			Title: "Old", Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour),
		}},
	}
	CalendarAgenda{M: past, Now: now}.Render(img, area)

	// Still draws the label/empty notice (ink present), and never panics.
	if !hasInk(img, area) {
		t.Error("empty agenda drew no ink at all (expected label + notice)")
	}
}
