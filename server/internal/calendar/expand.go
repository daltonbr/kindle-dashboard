package calendar

import "time"

// Occurrence is a concrete, materialised event instance ready for the agenda.
type Occurrence struct {
	Summary string
	Start   time.Time
	End     time.Time
	AllDay  bool
}

// Expand materialises the calendar into concrete occurrences whose time span
// overlaps [from, from+horizon). Recurring masters are expanded within that
// window (decision D20); EXDATEs, STATUS:CANCELLED, and RECURRENCE-ID overrides
// are applied so a moved or deleted instance shows correctly. The result is not
// sorted — data.CalendarModel.Upcoming handles ordering and the final N cut.
func Expand(cal Calendar, from time.Time, horizon time.Duration) []Occurrence {
	windowEnd := from.Add(horizon)

	// Index overrides by their series UID and the instant they replace, so a
	// master can suppress the original instance it stands in for.
	type ovKey struct {
		uid  string
		unix int64
	}
	overrides := map[ovKey]bool{}
	suppressByUID := map[string]map[int64]bool{}
	for _, e := range cal.Events {
		if e.RecurrenceID.IsZero() {
			continue
		}
		overrides[ovKey{e.UID, e.RecurrenceID.Unix()}] = true
		if suppressByUID[e.UID] == nil {
			suppressByUID[e.UID] = map[int64]bool{}
		}
		suppressByUID[e.UID][e.RecurrenceID.Unix()] = true
	}

	var out []Occurrence
	add := func(summary string, start, end time.Time, allDay bool) {
		// Keep anything still running or starting before the window closes.
		if end.After(from) && start.Before(windowEnd) {
			out = append(out, Occurrence{Summary: summary, Start: start, End: end, AllDay: allDay})
		}
	}

	for _, e := range cal.Events {
		if e.Cancelled || e.Start.IsZero() {
			continue
		}
		dur := e.End.Sub(e.Start)

		switch {
		case e.RRule != nil:
			for _, s := range e.RRule.between(e.Start, from, windowEnd, e.ExDates, suppressByUID[e.UID]) {
				add(e.Summary, s, s.Add(dur), e.AllDay)
			}
		default:
			// One-off, or a RECURRENCE-ID override standing in as its own event.
			add(e.Summary, e.Start, e.End, e.AllDay)
		}
	}

	return out
}
