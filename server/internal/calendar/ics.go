// Package calendar fetches and parses an iCalendar (RFC 5545) feed — in
// practice a Google Calendar "secret iCal URL" (decision D19) — into concrete,
// upcoming event occurrences for the agenda widget.
//
// Scope is deliberately bounded (decision D20): a hand-rolled VEVENT parser plus
// a recurrence expander that only materialises occurrences inside a small future
// window. That keeps the whole thing in the standard library — timezones resolve
// via time.LoadLocation against the embedded zoneinfo (see the time/tzdata blank
// import below), which is what lets it work inside the FROM scratch image that
// ships no tzdata files.
package calendar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	// Embed the IANA time zone database into the binary. The production image is
	// FROM scratch and carries no /usr/share/zoneinfo, so without this
	// time.LoadLocation("Europe/London") (used to resolve DTSTART;TZID=…) would
	// fail at runtime. ~450 KB, no third-party dependency.
	_ "time/tzdata"
)

// VEvent is a parsed VEVENT. A master recurring event has RRule != nil; a
// modified single instance of a series has RecurrenceID set; a one-off has
// neither.
type VEvent struct {
	UID          string
	Summary      string
	Start        time.Time
	End          time.Time
	AllDay       bool
	Cancelled    bool        // STATUS:CANCELLED
	RRule        *RRule      // nil if non-recurring
	ExDates      []time.Time // EXDATE instances to skip
	RecurrenceID time.Time   // non-zero → this overrides one instance of its UID's series
}

// Calendar is a parsed feed: the raw events plus the calendar-level default
// location (X-WR-TIMEZONE) used for floating times.
type Calendar struct {
	Events     []VEvent
	DefaultLoc *time.Location
	FetchedAt  time.Time
}

// Client fetches and parses one iCal feed.
type Client struct {
	url  string
	http *http.Client
	now  func() time.Time // injectable for tests
}

// NewClient builds a client for the given feed URL. A nil httpClient uses a
// default with a 10s timeout (matching the weather client's single-attempt
// policy, D12).
func NewClient(url string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{url: url, http: httpClient, now: time.Now}
}

// Fetch retrieves and parses the feed. It does a single attempt (no retries);
// the TTL Cache absorbs transient failures for the steady state (D12).
func (c *Client) Fetch(ctx context.Context) (Calendar, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return Calendar{}, fmt.Errorf("calendar: build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Calendar{}, fmt.Errorf("calendar: fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Calendar{}, fmt.Errorf("calendar: unexpected status %s", resp.Status)
	}

	// Feeds are small (a few hundred KB at most for a personal calendar); cap the
	// read so a misconfigured URL can't stream unbounded data into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Calendar{}, fmt.Errorf("calendar: read body: %w", err)
	}

	cal, err := Parse(string(body))
	if err != nil {
		return Calendar{}, err
	}
	cal.FetchedAt = c.now()
	return cal, nil
}

// Parse turns raw ICS text into a Calendar. Exported so tests (and the demo
// path) can parse fixtures without a network.
func Parse(text string) (Calendar, error) {
	lines := unfold(text)

	cal := Calendar{DefaultLoc: time.UTC}
	var cur *VEvent
	inEvent := false

	for _, raw := range lines {
		name, params, value := splitLine(raw)
		upper := strings.ToUpper(name)

		switch {
		case upper == "BEGIN" && strings.EqualFold(value, "VEVENT"):
			cur = &VEvent{}
			inEvent = true
			continue
		case upper == "END" && strings.EqualFold(value, "VEVENT"):
			if cur != nil {
				cal.Events = append(cal.Events, *cur)
			}
			cur = nil
			inEvent = false
			continue
		}

		if !inEvent {
			// Calendar-level properties we care about.
			if upper == "X-WR-TIMEZONE" {
				if loc, err := time.LoadLocation(strings.TrimSpace(value)); err == nil {
					cal.DefaultLoc = loc
				}
			}
			continue
		}

		switch upper {
		case "UID":
			cur.UID = value
		case "SUMMARY":
			cur.Summary = unescapeText(value)
		case "STATUS":
			cur.Cancelled = strings.EqualFold(strings.TrimSpace(value), "CANCELLED")
		case "DTSTART":
			t, allDay := parseICSTime(value, params, cal.DefaultLoc)
			cur.Start, cur.AllDay = t, allDay
		case "DTEND":
			t, _ := parseICSTime(value, params, cal.DefaultLoc)
			cur.End = t
		case "RECURRENCE-ID":
			t, _ := parseICSTime(value, params, cal.DefaultLoc)
			cur.RecurrenceID = t
		case "EXDATE":
			loc := paramLoc(params, cal.DefaultLoc)
			for _, v := range strings.Split(value, ",") {
				if t, _ := parseICSTime(strings.TrimSpace(v), params, loc); !t.IsZero() {
					cur.ExDates = append(cur.ExDates, t)
				}
			}
		case "RRULE":
			if r, err := parseRRule(value, cur.Start); err == nil {
				cur.RRule = r
			}
			// An unparseable/unsupported RRULE leaves RRule nil; the master then
			// renders as a single DTSTART occurrence (graceful degradation).
		}
	}

	// Default missing DTEND so duration math is well-defined.
	for i := range cal.Events {
		e := &cal.Events[i]
		if e.End.IsZero() && !e.Start.IsZero() {
			if e.AllDay {
				e.End = e.Start.AddDate(0, 0, 1) // all-day spans one day
			} else {
				e.End = e.Start // zero-length
			}
		}
	}

	return cal, nil
}

// unfold splits ICS text into logical lines, joining RFC 5545 line folds (a
// CRLF followed by a space or tab continues the previous line).
func unfold(text string) []string {
	rawLines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []string
	for _, l := range rawLines {
		if l == "" {
			continue
		}
		if (l[0] == ' ' || l[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		out = append(out, l)
	}
	return out
}

// splitLine parses "NAME;PARAM=VAL;PARAM=VAL:VALUE" into its parts. Parameter
// values may be DQUOTE-wrapped and contain colons; the value starts at the first
// unquoted colon.
func splitLine(line string) (name string, params map[string]string, value string) {
	colon := unquotedIndex(line, ':')
	head := line
	if colon >= 0 {
		head = line[:colon]
		value = line[colon+1:]
	}

	parts := splitUnquoted(head, ';')
	name = parts[0]
	params = map[string]string{}
	for _, p := range parts[1:] {
		if eq := strings.IndexByte(p, '='); eq >= 0 {
			k := strings.ToUpper(strings.TrimSpace(p[:eq]))
			v := strings.Trim(strings.TrimSpace(p[eq+1:]), `"`)
			params[k] = v
		}
	}
	return name, params, value
}

// unquotedIndex returns the index of the first occurrence of b outside DQUOTEs.
func unquotedIndex(s string, b byte) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case b:
			if !inQuote {
				return i
			}
		}
	}
	return -1
}

// splitUnquoted splits s on sep, ignoring separators inside DQUOTEs.
func splitUnquoted(s string, sep byte) []string {
	var out []string
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case sep:
			if !inQuote {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// parseICSTime parses a DATE or DATE-TIME value. Returns the resolved time and
// whether it was a date-only (all-day) value.
//
//	20260614T143000Z        -> UTC instant
//	20260614T143000 +TZID   -> in that zone
//	20260614T143000         -> floating, interpreted in defaultLoc
//	20260614 (VALUE=DATE)   -> all-day, midnight in defaultLoc/TZID
func parseICSTime(value string, params map[string]string, defaultLoc *time.Location) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}

	if strings.EqualFold(params["VALUE"], "DATE") || (len(value) == 8 && !strings.Contains(value, "T")) {
		if t, err := time.ParseInLocation("20060102", value, paramLoc(params, defaultLoc)); err == nil {
			return t, true
		}
		return time.Time{}, true
	}

	if strings.HasSuffix(value, "Z") {
		if t, err := time.ParseInLocation("20060102T150405Z", value, time.UTC); err == nil {
			return t.UTC(), false
		}
		return time.Time{}, false
	}

	if t, err := time.ParseInLocation("20060102T150405", value, paramLoc(params, defaultLoc)); err == nil {
		return t, false
	}
	return time.Time{}, false
}

// paramLoc resolves the TZID parameter to a location, falling back to def.
func paramLoc(params map[string]string, def *time.Location) *time.Location {
	if tzid := params["TZID"]; tzid != "" {
		if loc, err := time.LoadLocation(tzid); err == nil {
			return loc
		}
	}
	if def == nil {
		return time.UTC
	}
	return def
}

// unescapeText reverses RFC 5545 TEXT escaping (\n \, \; \\).
func unescapeText(s string) string {
	r := strings.NewReplacer(
		`\n`, "\n", `\N`, "\n",
		`\,`, ",", `\;`, ";", `\\`, `\`,
	)
	return r.Replace(s)
}
