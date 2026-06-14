package widgets

import (
	"image"
	"strings"
	"time"

	"golang.org/x/image/font"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

// CalendarAgenda is the 1×1 "what's next" card: a short list of upcoming events,
// one per row, each with a relative day/time label and its title. Past events
// are filtered out relative to Now.
type CalendarAgenda struct {
	M   data.CalendarModel
	Now time.Time
	// Max caps how many events to list (default 4 when zero).
	Max int
}

func (CalendarAgenda) Footprint() (cols, rows int) { return 1, 1 }

func (w CalendarAgenda) Render(dst *image.Gray, area image.Rectangle) {
	const pad = 16
	n := w.Max
	if n == 0 {
		n = 4
	}

	s := vscale(area)
	fs := func(px float64) float64 { return px * s }

	labelY := area.Min.Y + int(30*s)
	drawAt(dst, face(fs(24)), "AGENDA", area.Min.X+pad, labelY, 90)
	hLine(dst, area.Min.X+pad, area.Max.X-pad, labelY+int(10*s), 180)

	now := w.Now
	if now.IsZero() {
		now = w.M.FetchedAt
	}
	events := w.M.Upcoming(now, n)

	top := labelY + int(26*s)

	if len(events) == 0 {
		drawAt(dst, face(fs(26)), "Nothing scheduled", area.Min.X+pad, top+int(28*s), 60)
		return
	}

	// Divide the space below the header into n equal rows (by the cap, not the
	// event count) so the list stays top-aligned and rows never overflow the
	// cell — the bottom band the hardcoded height used to clip.
	avail := area.Max.Y - top - int(6*s)
	rowH := avail / n
	titleFace := face(fs(30))
	whenFace := face(fs(22))
	maxTitleW := area.Dx() - 2*pad

	for i, e := range events {
		rowTop := top + i*rowH

		drawAt(dst, titleFace, truncateToWidth(titleFace, e.Title, maxTitleW),
			area.Min.X+pad, rowTop+int(26*s), 0)
		drawAt(dst, whenFace, whenLabel(now, e),
			area.Min.X+pad, rowTop+int(50*s), 60)

		if i < len(events)-1 {
			hLine(dst, area.Min.X+pad, area.Max.X-pad, rowTop+rowH-int(4*s), 220)
		}
	}
}

// whenLabel renders an event's timing relative to now, e.g. "Today · 14:30",
// "Tomorrow · all day", "Mon · 09:00", "22 Jun · 18:00".
func whenLabel(now time.Time, e data.Event) string {
	day := dayLabel(now, e.Start)
	if e.AllDay {
		return day + " · all day"
	}
	return day + " · " + e.Start.In(now.Location()).Format("15:04")
}

// dayLabel describes start's calendar day relative to now's day.
func dayLabel(now, start time.Time) string {
	loc := now.Location()
	today := dateOnlyIn(now, loc)
	day := dateOnlyIn(start, loc)

	// Round to whole days to absorb DST-induced 23/25-hour spans.
	diff := int((day.Sub(today) + 12*time.Hour) / (24 * time.Hour))
	switch {
	case diff <= 0:
		return "Today"
	case diff == 1:
		return "Tomorrow"
	case diff < 7:
		return start.In(loc).Format("Mon")
	default:
		return start.In(loc).Format("2 Jan")
	}
}

func dateOnlyIn(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// truncateToWidth shortens s with a trailing ellipsis until it fits maxW pixels
// in face. Returns s unchanged if it already fits.
func truncateToWidth(f font.Face, s string, maxW int) string {
	if font.MeasureString(f, s).Round() <= maxW {
		return s
	}
	const ellipsis = "…"
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		candidate := strings.TrimRight(string(r), " ") + ellipsis
		if font.MeasureString(f, candidate).Round() <= maxW {
			return candidate
		}
	}
	return ellipsis
}
