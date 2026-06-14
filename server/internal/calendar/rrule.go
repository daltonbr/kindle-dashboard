package calendar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Freq is the supported subset of RRULE FREQ values.
type Freq int

const (
	FreqDaily Freq = iota
	FreqWeekly
	FreqMonthly
	FreqYearly
)

// RRule is the bounded subset of RFC 5545 recurrence we support (decision D20):
// FREQ DAILY/WEEKLY/MONTHLY/YEARLY with INTERVAL, COUNT, UNTIL, BYDAY (plain
// weekday list, no ordinals), and BYMONTHDAY. Exotic rules (BYSETPOS, ordinal
// BYDAY like 2MO, BYWEEKNO, …) are intentionally unsupported — parseRRule
// returns an error for them and the event degrades to a single occurrence.
type RRule struct {
	Freq       Freq
	Interval   int            // >= 1
	Count      int            // 0 = unbounded
	Until      time.Time      // zero = unbounded
	ByDay      []time.Weekday // FREQ=WEEKLY
	ByMonthDay []int          // FREQ=MONTHLY
}

// parseRRule parses an RRULE value. dtstart anchors UNTIL parsing for the rare
// date-only UNTIL form.
func parseRRule(value string, dtstart time.Time) (*RRule, error) {
	r := &RRule{Interval: 1}
	freqSet := false

	for _, part := range strings.Split(value, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(kv[0]))
		val := strings.TrimSpace(kv[1])

		switch key {
		case "FREQ":
			switch strings.ToUpper(val) {
			case "DAILY":
				r.Freq = FreqDaily
			case "WEEKLY":
				r.Freq = FreqWeekly
			case "MONTHLY":
				r.Freq = FreqMonthly
			case "YEARLY":
				r.Freq = FreqYearly
			default:
				return nil, fmt.Errorf("rrule: unsupported FREQ %q", val)
			}
			freqSet = true
		case "INTERVAL":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("rrule: bad INTERVAL %q", val)
			}
			r.Interval = n
		case "COUNT":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("rrule: bad COUNT %q", val)
			}
			r.Count = n
		case "UNTIL":
			t, _ := parseICSTime(val, nil, dtstart.Location())
			if t.IsZero() {
				return nil, fmt.Errorf("rrule: bad UNTIL %q", val)
			}
			r.Until = t
		case "BYDAY":
			for _, d := range strings.Split(val, ",") {
				d = strings.TrimSpace(d)
				// Reject ordinal prefixes (e.g. 2MO, -1FR): unsupported.
				if len(d) != 2 {
					return nil, fmt.Errorf("rrule: unsupported BYDAY %q", d)
				}
				wd, ok := weekdayFromICS(d)
				if !ok {
					return nil, fmt.Errorf("rrule: bad BYDAY %q", d)
				}
				r.ByDay = append(r.ByDay, wd)
			}
		case "BYMONTHDAY":
			for _, d := range strings.Split(val, ",") {
				n, err := strconv.Atoi(strings.TrimSpace(d))
				if err != nil || n < 1 || n > 31 {
					return nil, fmt.Errorf("rrule: unsupported BYMONTHDAY %q", d)
				}
				r.ByMonthDay = append(r.ByMonthDay, n)
			}
		case "WKST":
			// Week-start only affects multi-week INTERVAL alignment; ignored for
			// our bounded window. Not an error.
		default:
			// Any rule part we don't model (BYSETPOS, BYWEEKNO, BYYEARDAY, …)
			// makes the result unreliable — refuse rather than silently mislead.
			return nil, fmt.Errorf("rrule: unsupported part %q", key)
		}
	}

	if !freqSet {
		return nil, fmt.Errorf("rrule: missing FREQ")
	}
	return r, nil
}

// maxOccurrences caps expansion so a pathological feed can't spin forever. At
// daily frequency this is ~27 years of stepping — far beyond any agenda window.
const maxOccurrences = 10000

// between returns the occurrence start times of a recurring event that fall in
// [windowStart, windowEnd], excluding exdates and suppressed instants. dtstart
// is the series anchor (also the first candidate). Time-of-day and location are
// preserved from dtstart, so DST transitions resolve correctly.
func (r *RRule) between(dtstart, windowStart, windowEnd time.Time, exdates []time.Time, suppress map[int64]bool) []time.Time {
	var out []time.Time
	count := 0

	emit := func(t time.Time) bool {
		count++
		if r.Count > 0 && count > r.Count {
			return false
		}
		if !r.Until.IsZero() && t.After(r.Until) {
			return false
		}
		if t.After(windowEnd) {
			// For frequencies that advance monotonically we could stop, but
			// BYDAY can emit several per step; the caller's step bound handles
			// termination, so just skip out-of-window candidates here.
			return true
		}
		if t.Before(windowStart) {
			return true
		}
		if isExcluded(t, exdates) || (suppress != nil && suppress[t.Unix()]) {
			return true
		}
		out = append(out, t)
		return true
	}

	switch r.Freq {
	case FreqWeekly:
		days := r.ByDay
		if len(days) == 0 {
			days = []time.Weekday{dtstart.Weekday()}
		}
		// Walk week by week from dtstart's week; within each active week emit the
		// listed weekdays (at dtstart's time-of-day).
		weekAnchor := startOfWeek(dtstart)
		for i := 0; i < maxOccurrences; i++ {
			weekStart := weekAnchor.AddDate(0, 0, 7*r.Interval*i)
			if weekStart.After(windowEnd) {
				break
			}
			stop := false
			for _, wd := range days {
				occ := atTimeOf(dtOnWeekday(weekStart, wd), dtstart)
				if occ.Before(dtstart) {
					continue
				}
				if !emit(occ) {
					stop = true
					break
				}
			}
			if stop {
				break
			}
		}

	case FreqMonthly:
		mdays := r.ByMonthDay
		if len(mdays) == 0 {
			mdays = []int{dtstart.Day()}
		}
		// Step by calendar month explicitly. Using AddDate on a day-31 anchor
		// would let Go normalise overflow days (Feb 31 -> Mar 3), landing in the
		// wrong month and double-counting the next one.
		baseMonth := int(dtstart.Month()) - 1 // 0-based months since year 0
		for i := 0; i < maxOccurrences; i++ {
			total := dtstart.Year()*12 + baseMonth + r.Interval*i
			yy, mm := total/12, time.Month(total%12+1)
			if time.Date(yy, mm, 1, 0, 0, 0, 0, dtstart.Location()).After(windowEnd) {
				break
			}
			stop := false
			for _, md := range mdays {
				occ, ok := dayOfMonth(yy, mm, md, dtstart)
				if !ok || occ.Before(dtstart) {
					continue
				}
				if !emit(occ) {
					stop = true
					break
				}
			}
			if stop {
				break
			}
		}

	case FreqYearly:
		for i := 0; i < maxOccurrences; i++ {
			occ := dtstart.AddDate(r.Interval*i, 0, 0)
			if occ.After(windowEnd) && r.Count == 0 && r.Until.IsZero() {
				break
			}
			if !emit(occ) {
				break
			}
		}

	default: // FreqDaily
		for i := 0; i < maxOccurrences; i++ {
			occ := dtstart.AddDate(0, 0, r.Interval*i)
			if occ.After(windowEnd) && r.Count == 0 && r.Until.IsZero() {
				break
			}
			if !emit(occ) {
				break
			}
		}
	}

	return out
}

func isExcluded(t time.Time, exdates []time.Time) bool {
	for _, ex := range exdates {
		if ex.Equal(t) || sameWallDay(ex, t) {
			return true
		}
	}
	return false
}

func sameWallDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd && a.Hour() == b.Hour() && a.Minute() == b.Minute()
}

func weekdayFromICS(s string) (time.Weekday, bool) {
	switch strings.ToUpper(s) {
	case "SU":
		return time.Sunday, true
	case "MO":
		return time.Monday, true
	case "TU":
		return time.Tuesday, true
	case "WE":
		return time.Wednesday, true
	case "TH":
		return time.Thursday, true
	case "FR":
		return time.Friday, true
	case "SA":
		return time.Saturday, true
	}
	return 0, false
}

// startOfWeek returns midnight on the Sunday of t's week, in t's location.
func startOfWeek(t time.Time) time.Time {
	d := dateOnly(t)
	return d.AddDate(0, 0, -int(d.Weekday()))
}

// dtOnWeekday returns the date of weekday wd within the week beginning weekStart.
func dtOnWeekday(weekStart time.Time, wd time.Weekday) time.Time {
	return weekStart.AddDate(0, 0, int(wd))
}

// atTimeOf returns date's calendar day at ref's time-of-day, in ref's location.
func atTimeOf(date, ref time.Time) time.Time {
	y, m, d := date.Date()
	return time.Date(y, m, d, ref.Hour(), ref.Minute(), ref.Second(), 0, ref.Location())
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// dayOfMonth returns ref's time-of-day on day md of year yy / month mm, or
// ok=false if that month has no such day (e.g. day 31 in February).
func dayOfMonth(yy int, mm time.Month, md int, ref time.Time) (time.Time, bool) {
	t := time.Date(yy, mm, md, ref.Hour(), ref.Minute(), ref.Second(), 0, ref.Location())
	if t.Month() != mm { // Go normalised an overflow day into the next month
		return time.Time{}, false
	}
	return t, true
}
