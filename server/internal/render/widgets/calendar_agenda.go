package widgets

import (
	"image"
	"strings"
	"time"

	"golang.org/x/image/font"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

// CalendarAgenda is the 1×1 "what's next" card: a short list of upcoming events,
// each with its title (wrapped to at most two lines) and a relative day/time
// label. Past events are filtered out relative to Now.
type CalendarAgenda struct {
	M   data.CalendarModel
	Now time.Time
	// Max caps how many events to list (default 3 when zero). Titles wrap to
	// two lines when needed, so fewer fit than the old single-line layout.
	Max int
}

// maxTitleLines bounds how tall a single event's title may grow.
const maxTitleLines = 2

// defaultAgendaEvents is the agenda's default event cap. CalendarMonth reads it
// so its "later" footer can skip exactly what the agenda already shows, letting
// the two tiles complement rather than duplicate each other.
const defaultAgendaEvents = 3

func (CalendarAgenda) Footprint() (cols, rows int) { return 1, 1 }

func (w CalendarAgenda) Render(dst *image.Gray, area image.Rectangle) {
	const pad = 16
	n := w.Max
	if n == 0 {
		n = defaultAgendaEvents
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

	// Flow events top-down with variable heights: a title wraps to one or two
	// lines, then its when-label sits below. We always draw the first event;
	// later ones are dropped once a block would overflow the cell, so the list
	// never clips mid-row.
	bottom := area.Max.Y - int(6*s)
	titleFace := face(fs(30))
	whenFace := face(fs(22))
	titleLineH := int(33 * s)
	maxTitleW := area.Dx() - 2*pad

	y := top
	for i, e := range events {
		lines := wrapToWidth(titleFace, sanitizeTitle(e.Title), maxTitleW, maxTitleLines)
		blockH := len(lines)*titleLineH + int(30*s) // title lines + when-label row
		if i > 0 && y+blockH > bottom {
			break
		}

		base := y + int(24*s)
		for _, ln := range lines {
			drawAt(dst, titleFace, ln, area.Min.X+pad, base, 0)
			base += titleLineH
		}
		drawAt(dst, whenFace, whenLabel(now, e), area.Min.X+pad, base+int(2*s), 60)

		y += blockH
		if i < len(events)-1 && y+int(8*s) < bottom {
			hLine(dst, area.Min.X+pad, area.Max.X-pad, y, 220)
			y += int(8 * s)
		}
	}
}

// sanitizeTitle drops runes the display font can't render (emoji and other
// pictographs, which would otherwise show as blank "tofu" boxes on the panel)
// and collapses the whitespace they leave behind.
func sanitizeTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if hasGlyph(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// wrapToWidth lays s out across at most maxLines lines, each fitting maxW pixels
// in face. Returns a single line unchanged when it already fits. The final line
// is ellipsised when the text still overflows; a lone word wider than maxW is
// hard-truncated.
func wrapToWidth(f font.Face, s string, maxW, maxLines int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	cur := ""
	for i := 0; i < len(words); {
		try := words[i]
		if cur != "" {
			try = cur + " " + words[i]
		}
		if font.MeasureString(f, try).Round() <= maxW {
			cur = try
			i++
			continue
		}
		if cur == "" { // single word too wide for an empty line
			cur = truncateToWidth(f, words[i], maxW)
			i++
		}
		lines = append(lines, cur)
		cur = ""
		if len(lines) == maxLines {
			// No room left: fold the remainder into the last line as an
			// ellipsised overflow hint.
			if rest := strings.Join(words[i:], " "); rest != "" {
				lines[maxLines-1] = truncateToWidth(f, lines[maxLines-1]+" "+rest, maxW)
			}
			return lines
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
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
