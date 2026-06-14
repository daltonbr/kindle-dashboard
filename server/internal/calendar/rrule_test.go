package calendar

import (
	"testing"
	"time"
)

func TestParseRRule_supported(t *testing.T) {
	dt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	r, err := parseRRule("FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;COUNT=10", dt)
	if err != nil {
		t.Fatal(err)
	}
	if r.Freq != FreqWeekly || r.Interval != 2 || r.Count != 10 {
		t.Errorf("parsed %+v", r)
	}
	if len(r.ByDay) != 2 || r.ByDay[0] != time.Monday || r.ByDay[1] != time.Wednesday {
		t.Errorf("ByDay = %v", r.ByDay)
	}
}

func TestParseRRule_unsupported(t *testing.T) {
	dt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	cases := []string{
		"FREQ=WEEKLY;BYDAY=2MO",    // ordinal BYDAY
		"FREQ=MONTHLY;BYSETPOS=-1", // BYSETPOS
		"FREQ=HOURLY",              // unsupported FREQ
		"FREQ=YEARLY;BYWEEKNO=20",  // BYWEEKNO
		"INTERVAL=2",               // missing FREQ
		"FREQ=DAILY;BYYEARDAY=100", // BYYEARDAY
	}
	for _, c := range cases {
		if _, err := parseRRule(c, dt); err == nil {
			t.Errorf("parseRRule(%q) = nil error, want unsupported", c)
		}
	}
}

func dates(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Format("2006-01-02 15:04")
	}
	return out
}

func TestRRule_betweenWeeklyBYDAY(t *testing.T) {
	loc := time.UTC
	dt := time.Date(2026, 6, 1, 9, 0, 0, 0, loc) // Monday
	r, _ := parseRRule("FREQ=WEEKLY;BYDAY=MO,WE,FR", dt)

	from := time.Date(2026, 6, 15, 0, 0, 0, 0, loc) // Monday
	to := time.Date(2026, 6, 21, 0, 0, 0, 0, loc)   // following Sunday
	got := dates(r.between(dt, from, to, nil, nil))

	want := []string{"2026-06-15 09:00", "2026-06-17 09:00", "2026-06-19 09:00"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("occ[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestRRule_betweenEXDATE(t *testing.T) {
	loc := time.UTC
	dt := time.Date(2026, 6, 1, 9, 0, 0, 0, loc)
	r, _ := parseRRule("FREQ=WEEKLY;BYDAY=MO,WE,FR", dt)
	ex := []time.Time{time.Date(2026, 6, 17, 9, 0, 0, 0, loc)} // skip the Wed

	from := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)
	to := time.Date(2026, 6, 21, 0, 0, 0, 0, loc)
	got := dates(r.between(dt, from, to, ex, nil))

	want := []string{"2026-06-15 09:00", "2026-06-19 09:00"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRRule_betweenCOUNT(t *testing.T) {
	loc := time.UTC
	dt := time.Date(2026, 6, 1, 9, 0, 0, 0, loc)
	r, _ := parseRRule("FREQ=DAILY;COUNT=3", dt)

	// Window wide open; COUNT caps the series at 3 (06-01, 06-02, 06-03).
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 7, 1, 0, 0, 0, 0, loc)
	got := dates(r.between(dt, from, to, nil, nil))

	want := []string{"2026-06-01 09:00", "2026-06-02 09:00", "2026-06-03 09:00"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRRule_betweenUNTIL(t *testing.T) {
	loc := time.UTC
	dt := time.Date(2026, 6, 1, 9, 0, 0, 0, loc)
	r, _ := parseRRule("FREQ=DAILY;UNTIL=20260603T090000Z", dt)

	from := time.Date(2026, 6, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 6, 30, 0, 0, 0, 0, loc)
	got := r.between(dt, from, to, nil, nil)
	if len(got) != 3 { // 06-01, 06-02, 06-03 inclusive of UNTIL
		t.Errorf("UNTIL series = %v, want 3 occurrences", dates(got))
	}
}

func TestRRule_betweenMonthlyByMonthDay(t *testing.T) {
	loc := time.UTC
	dt := time.Date(2026, 1, 15, 8, 0, 0, 0, loc)
	r, _ := parseRRule("FREQ=MONTHLY;BYMONTHDAY=15", dt)

	from := time.Date(2026, 6, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 8, 31, 0, 0, 0, 0, loc)
	got := dates(r.between(dt, from, to, nil, nil))

	want := []string{"2026-06-15 08:00", "2026-07-15 08:00", "2026-08-15 08:00"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRRule_betweenMonthlySkipsShortMonth(t *testing.T) {
	loc := time.UTC
	dt := time.Date(2026, 1, 31, 8, 0, 0, 0, loc)
	r, _ := parseRRule("FREQ=MONTHLY;BYMONTHDAY=31", dt)

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 4, 30, 0, 0, 0, 0, loc)
	got := dates(r.between(dt, from, to, nil, nil))

	// Feb has no 31st; only March qualifies in the window.
	want := []string{"2026-03-31 08:00"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("got %v, want %v (Feb 31 must be skipped)", got, want)
	}
}

func TestRRule_betweenYearly(t *testing.T) {
	loc := time.UTC
	dt := time.Date(2000, 7, 4, 0, 0, 0, 0, loc) // a birthday
	r, _ := parseRRule("FREQ=YEARLY", dt)

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, loc)
	got := dates(r.between(dt, from, to, nil, nil))
	if len(got) != 1 || got[0] != "2026-07-04 00:00" {
		t.Errorf("yearly got %v, want one 2026-07-04", got)
	}
}

func TestRRule_dstPreservesWallTime(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skip("no tzdata")
	}
	// A 09:00 daily event spanning the spring-forward (29 Mar 2026 in the UK).
	dt := time.Date(2026, 3, 28, 9, 0, 0, 0, loc)
	r, _ := parseRRule("FREQ=DAILY", dt)
	from := time.Date(2026, 3, 28, 0, 0, 0, 0, loc)
	to := time.Date(2026, 3, 31, 0, 0, 0, 0, loc)
	got := r.between(dt, from, to, nil, nil)
	for _, occ := range got {
		if occ.Hour() != 9 {
			t.Errorf("occurrence %v lost its 09:00 wall time across DST", occ)
		}
	}
}
