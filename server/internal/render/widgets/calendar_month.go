package widgets

import (
	"image"
	"strconv"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

// CalendarMonth is the 1×1 spatial companion to CalendarAgenda: a rolling
// multi-week grid (Monday-first) that starts on the current week, highlights
// today, and marks days that have an event with a dot. Below the grid a short
// "later" list shows the next events beyond the ones the agenda already lists,
// so the two tiles complement rather than duplicate each other.
type CalendarMonth struct {
	M   data.CalendarModel
	Now time.Time
	// Weeks is how many week rows to draw (default 4).
	Weeks int
}

func (CalendarMonth) Footprint() (cols, rows int) { return 1, 1 }

var weekdayHeads = [7]string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}

func (w CalendarMonth) Render(dst *image.Gray, area image.Rectangle) {
	const pad = 16
	weeks := w.Weeks
	if weeks == 0 {
		weeks = 4
	}

	s := vscale(area)
	fs := func(px float64) float64 { return px * s }

	now := w.Now
	if now.IsZero() {
		now = w.M.FetchedAt
	}
	loc := now.Location()
	today := dateOnlyIn(now, loc)

	// Start on the Monday of the current week (Weekday: Sun=0..Sat=6).
	offset := (int(today.Weekday()) + 6) % 7
	gridStart := today.AddDate(0, 0, -offset)
	gridEnd := gridStart.AddDate(0, 0, weeks*7-1)

	// Header: month name, or a spanning "Jan – Feb" form when the visible weeks
	// straddle a month boundary.
	label := gridStart.Format("January")
	if gridEnd.Month() != gridStart.Month() {
		label = gridStart.Format("Jan") + " – " + gridEnd.Format("Jan")
	}
	labelY := area.Min.Y + int(30*s)
	drawAt(dst, face(fs(24)), label, area.Min.X+pad, labelY, 90)
	hLine(dst, area.Min.X+pad, area.Max.X-pad, labelY+int(10*s), 180)

	eventDays := w.eventDaySet(loc)

	// Reserve a footer band for the "later" list; the grid fills the space
	// between the weekday header and that band.
	later := w.laterEvents(now)
	footerH := 0
	if len(later) > 0 {
		footerH = int(20*s) + len(later)*int(24*s)
	}

	colW := (area.Dx() - 2*pad) / 7
	gridLeft := area.Min.X + pad
	headBaseY := labelY + int(34*s)
	weekdayFace := face(fs(16))
	for c := 0; c < 7; c++ {
		drawCentered(dst, weekdayFace, weekdayHeads[c], gridLeft+c*colW+colW/2, headBaseY, 130)
	}

	gridTop := headBaseY + int(8*s)
	gridBottom := area.Max.Y - footerH - int(6*s)
	rowH := (gridBottom - gridTop) / weeks
	numFace := face(fs(22))

	// Light rule before the Saturday column (index 5), setting the weekend apart.
	vLine(dst, gridLeft+5*colW, headBaseY-int(14*s), gridBottom, 210)

	for i := range weeks * 7 {
		day := gridStart.AddDate(0, 0, i)
		col, row := i%7, i/7
		cx := gridLeft + col*colW + colW/2
		cellTop := gridTop + row*rowH
		numBase := cellTop + int(float64(rowH)*0.52)

		switch {
		case day.Equal(today):
			// Today: filled chip behind a white number.
			side := int(26 * s)
			fillBar(dst, image.Rect(cx-side/2, numBase-int(19*s), cx+side/2, numBase+int(7*s)), 60)
			drawCentered(dst, numFace, strconv.Itoa(day.Day()), cx, numBase, 255)
		case day.Before(today):
			drawCentered(dst, numFace, strconv.Itoa(day.Day()), cx, numBase, 175) // past: dimmed
		default:
			drawCentered(dst, numFace, strconv.Itoa(day.Day()), cx, numBase, 30)
		}

		if eventDays[day] && !day.Equal(today) {
			dot := int(4 * s)
			dotY := cellTop + int(float64(rowH)*0.78)
			fillBar(dst, image.Rect(cx-dot/2, dotY-dot/2, cx-dot/2+dot, dotY-dot/2+dot), 80)
		}
	}

	if len(later) == 0 {
		return
	}

	// "Later" footer: next events beyond the agenda's window, one compact line
	// each ("Sat · Weekend trip").
	hLine(dst, area.Min.X+pad, area.Max.X-pad, gridBottom+int(4*s), 220)
	laterFace := face(fs(20))
	maxW := area.Dx() - 2*pad
	y := gridBottom + int(22*s)
	for _, e := range later {
		line := dayLabel(now, e.Start) + " · " + sanitizeTitle(e.Title)
		drawAt(dst, laterFace, truncateToWidth(laterFace, line, maxW), area.Min.X+pad, y, 60)
		y += int(24 * s)
	}
}

// eventDaySet returns the set of calendar days (in loc) that have at least one
// event, keyed by the day's midnight. Only the event's start day is marked.
func (w CalendarMonth) eventDaySet(loc *time.Location) map[time.Time]bool {
	days := make(map[time.Time]bool, len(w.M.Events))
	for _, e := range w.M.Events {
		days[dateOnlyIn(e.Start, loc)] = true
	}
	return days
}

// laterEvents returns the upcoming events that fall after the agenda's window,
// capped to a short tail so the footer stays one or two lines.
func (w CalendarMonth) laterEvents(now time.Time) []data.Event {
	const footerMax = 2
	up := w.M.Upcoming(now, defaultAgendaEvents+footerMax)
	if len(up) <= defaultAgendaEvents {
		return nil
	}
	return up[defaultAgendaEvents:]
}
