package main

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/teambition/rrule-go"
)

// minUpcoming: on a quiet week keep looking ahead until this many events are listed.
const minUpcoming = 6

// CalendarSource downloads the iCal feeds and turns them into an Agenda.
type CalendarSource struct {
	cfg  *Config
	demo bool

	mu        sync.Mutex
	events    []Event
	err       string
	fetchedAt time.Time
}

func NewCalendarSource(cfg *Config, demo bool) *CalendarSource {
	return &CalendarSource{cfg: cfg, demo: demo}
}

// Stale reports whether a refresh is due.
func (s *CalendarSource) Stale(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetchedAt.IsZero() || now.Sub(s.fetchedAt) > time.Duration(s.cfg.CalendarTTLSeconds)*time.Second
}

// Refresh fetches every feed in parallel. A broken feed must not hide the working ones.
func (s *CalendarSource) Refresh(now time.Time) {
	var events []Event
	var failed []string
	if s.demo {
		events = demoEvents(now, s.cfg.Loc)
	} else {
		events, failed = fetchAll(s.cfg, now)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetchedAt = now
	if len(failed) > 0 && len(failed) == len(s.cfg.Calendars) && len(s.events) > 0 {
		s.err = strings.Join(failed, "; ") // keep showing the last good data
		return
	}
	s.events, s.err = events, strings.Join(failed, "; ")
}

func (s *CalendarSource) Agenda(now time.Time) Agenda {
	s.mu.Lock()
	events, errText := s.events, s.err
	s.mu.Unlock()
	return Agenda{Days: buildDays(events, now, s.cfg), Events: events, Demo: s.demo, Err: errText}
}

func fetchAll(cfg *Config, now time.Time) ([]Event, []string) {
	winStart, winEnd := browseWindow(cfg, now)

	type result struct {
		events []Event
		err    string
	}
	results := make([]result, len(cfg.Calendars))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i, spec := range cfg.Calendars {
		wg.Add(1)
		go func(i int, spec CalendarSpec) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			name := spec.Name
			if name == "" {
				name = fmt.Sprintf("calendar %d", i+1)
			}
			data, err := fetchICS(spec.URL)
			if err != nil {
				results[i].err = name + ": " + safeErr(err)
				return
			}
			evs, err := eventsFromICS(data, name, winStart, winEnd, cfg.Loc)
			if err != nil {
				results[i].err = name + ": " + safeErr(err)
				return
			}
			results[i].events = evs
		}(i, spec)
	}
	wg.Wait()

	var all []Event
	var failed []string
	for _, r := range results {
		all = append(all, r.events...)
		if r.err != "" {
			failed = append(failed, r.err)
		}
	}
	return dedupe(all), failed
}

// browseWindow is the span events are expanded for: what the Calendar screen can browse, and at least
// the agenda's own look-ahead.
func browseWindow(cfg *Config, now time.Time) (start, end time.Time) {
	n := now.In(cfg.Loc)
	first := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, cfg.Loc)
	start = first.AddDate(0, -cfg.CalendarBrowseBack, 0)
	end = first.AddDate(0, cfg.CalendarBrowseAhead+1, 0)
	if agendaEnd := midnight(n, cfg.Loc).AddDate(0, 0, cfg.CalendarLookahead+2); agendaEnd.After(end) {
		end = agendaEnd
	}
	return start, end
}

// safeErr drops the URL from network errors: the iCal address is a secret.
func safeErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return err.Error()
}

func fetchICS(u string) ([]byte, error) {
	client := &http.Client{Timeout: 40 * time.Second}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kindle-dash/2.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

func dedupe(events []Event) []Event {
	seen := map[string]bool{}
	out := events[:0:0]
	for _, e := range events {
		k := fmt.Sprintf("%s|%d|%d|%v", e.Title, e.Start.Unix(), e.End.Unix(), e.AllDay)
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}

// --- iCal parsing -----------------------------------------------------------

type vevent struct {
	uid, title, loc string
	desc            string
	start, end      time.Time // in the event's own zone, so repeats keep their wall-clock time
	allDay          bool
	cancelled       bool
	rrule           string
	exdates         map[string]bool
	rdates          []time.Time
	recurrenceKey   string // set on an edited instance of a repeating event
}

func eventsFromICS(data []byte, calName string, winStart, winEnd time.Time, loc *time.Location) ([]Event, error) {
	cal, err := ics.ParseCalendar(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var masters, overrides []*vevent
	for _, ev := range cal.Events() {
		v, ok := parseVEvent(ev, loc)
		if !ok {
			continue
		}
		if v.recurrenceKey != "" {
			overrides = append(overrides, v)
		} else {
			masters = append(masters, v)
		}
	}
	replaced := map[string]bool{} // uid|instance key, replaced by an edited instance
	for _, o := range overrides {
		replaced[o.uid+"|"+o.recurrenceKey] = true
	}

	var out []Event
	add := func(v *vevent, start, end time.Time) {
		if v.cancelled {
			return
		}
		if !start.Before(winEnd) || !maxTime(end, start.Add(time.Second)).After(winStart) {
			return
		}
		out = append(out, Event{Title: v.title, Location: v.loc, Cal: calName, Desc: v.desc,
			Start: start.In(loc), End: end.In(loc), AllDay: v.allDay})
	}

	for _, m := range masters {
		if m.rrule == "" && len(m.rdates) == 0 {
			add(m, m.start, m.end)
			continue
		}
		var starts []time.Time
		if m.rrule != "" {
			opt, err := rrule.StrToROptionInLocation(m.rrule, m.start.Location())
			if err != nil {
				add(m, m.start, m.end) // unreadable rule: show the first instance rather than nothing
				continue
			}
			opt.Dtstart = m.start
			rr, err := rrule.NewRRule(*opt)
			if err != nil {
				add(m, m.start, m.end)
				continue
			}
			starts = rr.Between(winStart.Add(-m.end.Sub(m.start)-time.Hour), winEnd, true)
		}
		starts = append(starts, m.rdates...)
		for _, s := range starts {
			key := instanceKey(s, m.allDay)
			if m.exdates[key] || replaced[m.uid+"|"+key] {
				continue
			}
			add(m, s, instanceEnd(m, s))
		}
	}
	for _, o := range overrides {
		add(o, o.start, o.end)
	}
	return out, nil
}

func parseVEvent(ev *ics.VEvent, loc *time.Location) (*vevent, bool) {
	startProp := ev.GetProperty(ics.ComponentPropertyDtStart)
	if startProp == nil {
		return nil, false
	}
	start, allDay, err := parseICSTime(startProp.Value, startProp.ICalParameters, loc)
	if err != nil {
		return nil, false
	}
	v := &vevent{start: start, allDay: allDay, exdates: map[string]bool{}}
	if p := ev.GetProperty(ics.ComponentPropertyUniqueId); p != nil {
		v.uid = p.Value
	}
	v.title = textValue(ev, ics.ComponentPropertySummary)
	v.loc = textValue(ev, ics.ComponentPropertyLocation)
	if p := ev.GetProperty(ics.ComponentPropertyDescription); p != nil {
		v.desc = cleanDescription(p.Value)
	}
	v.cancelled = strings.EqualFold(strings.TrimSpace(textValue(ev, ics.ComponentPropertyStatus)), "CANCELLED")

	switch {
	case ev.GetProperty(ics.ComponentPropertyDtEnd) != nil:
		p := ev.GetProperty(ics.ComponentPropertyDtEnd)
		if end, _, err := parseICSTime(p.Value, p.ICalParameters, loc); err == nil {
			v.end = end
		}
	case ev.GetProperty(ics.ComponentPropertyDuration) != nil:
		if d, ok := parseDuration(ev.GetProperty(ics.ComponentPropertyDuration).Value); ok {
			v.end = start.Add(d)
		}
	}
	if v.end.IsZero() {
		if allDay {
			v.end = start.AddDate(0, 0, 1)
		} else {
			v.end = start
		}
	}
	if p := ev.GetProperty(ics.ComponentPropertyRrule); p != nil {
		v.rrule = p.Value
	}
	for _, p := range ev.GetProperties(ics.ComponentPropertyExdate) {
		for _, val := range strings.Split(p.Value, ",") {
			if t, ad, err := parseICSTime(val, p.ICalParameters, loc); err == nil {
				v.exdates[instanceKey(t, ad)] = true
			}
		}
	}
	for _, p := range ev.GetProperties(ics.ComponentPropertyRdate) {
		for _, val := range strings.Split(p.Value, ",") {
			if t, _, err := parseICSTime(val, p.ICalParameters, loc); err == nil {
				v.rdates = append(v.rdates, t)
			}
		}
	}
	if p := ev.GetProperty(ics.ComponentPropertyRecurrenceId); p != nil {
		if t, ad, err := parseICSTime(p.Value, p.ICalParameters, loc); err == nil {
			v.recurrenceKey = instanceKey(t, ad)
		}
	}
	return v, true
}

func textValue(ev *ics.VEvent, prop ics.ComponentProperty) string {
	p := ev.GetProperty(prop)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(unescapeText(p.Value))
}

func unescapeText(s string) string {
	r := strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`)
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}

var (
	htmlBreak = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h[1-6]|/tr)\s*/?>`)
	htmlTag   = regexp.MustCompile(`<[^>]*>`)
)

const maxDescription = 3000

// cleanDescription turns an iCal DESCRIPTION into plain text: escapes undone, simple HTML flattened,
// paragraphs kept as line breaks, blank runs collapsed, and the length capped.
func cleanDescription(raw string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	t := r.Replace(raw)
	t = htmlBreak.ReplaceAllString(t, "\n")
	t = htmlTag.ReplaceAllString(t, "")
	t = html.UnescapeString(t)
	var out []string
	blank := true // drops leading blank lines
	for _, line := range strings.Split(strings.ReplaceAll(t, "\r", ""), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, line)
		blank = false
	}
	t = strings.TrimSpace(strings.Join(out, "\n"))
	if rs := []rune(t); len(rs) > maxDescription {
		t = strings.TrimSpace(string(rs[:maxDescription])) + "…"
	}
	return t
}

func first(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}

// parseICSTime keeps the zone the event was written in. Dates and floating times use loc.
func parseICSTime(value string, params map[string][]string, loc *time.Location) (time.Time, bool, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(first(params["VALUE"]), "DATE") || len(value) == 8 {
		if len(value) < 8 {
			return time.Time{}, false, fmt.Errorf("bad date %q", value)
		}
		t, err := time.ParseInLocation("20060102", value[:8], loc)
		return t, true, err
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.ParseInLocation("20060102T150405Z", value, time.UTC)
		return t, false, err
	}
	zone := loc
	if tzid := strings.Trim(first(params["TZID"]), `"`); tzid != "" {
		if z, err := time.LoadLocation(tzid); err == nil {
			zone = z
		}
	}
	t, err := time.ParseInLocation("20060102T150405", value, zone)
	return t, false, err
}

func instanceKey(t time.Time, allDay bool) string {
	if allDay {
		return "d" + t.Format("20060102")
	}
	return "t" + strconv.FormatInt(t.Unix(), 10)
}

func instanceEnd(m *vevent, start time.Time) time.Time {
	if m.allDay {
		days := int(m.end.Sub(m.start).Round(24*time.Hour) / (24 * time.Hour))
		return start.AddDate(0, 0, days)
	}
	return start.Add(m.end.Sub(m.start))
}

// parseDuration reads an iCal duration such as PT1H30M, P1D or P2W.
func parseDuration(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	sign := time.Duration(1)
	if strings.HasPrefix(s, "-") {
		sign, s = -1, s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") {
		return 0, false
	}
	var total time.Duration
	inTime := false
	num := ""
	for _, r := range s[1:] {
		switch {
		case r >= '0' && r <= '9':
			num += string(r)
		case r == 'T':
			inTime = true
		default:
			n, err := strconv.Atoi(num)
			if err != nil {
				return 0, false
			}
			num = ""
			switch {
			case r == 'W':
				total += time.Duration(n) * 7 * 24 * time.Hour
			case r == 'D':
				total += time.Duration(n) * 24 * time.Hour
			case r == 'H' && inTime:
				total += time.Duration(n) * time.Hour
			case r == 'M' && inTime:
				total += time.Duration(n) * time.Minute
			case r == 'S' && inTime:
				total += time.Duration(n) * time.Second
			default:
				return 0, false
			}
		}
	}
	return sign * total, true
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func midnight(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// --- grouping into days -----------------------------------------------------

// rowsForDay lists what happens on one day, all-day events first, then by start time.
func rowsForDay(events []Event, dayStart time.Time, cfg *Config) []Row {
	dayEnd := dayStart.AddDate(0, 0, 1)
	var rows []Row
	for _, e := range events {
		end := maxTime(e.End, e.Start.Add(time.Second))
		if e.Start.Before(dayEnd) && end.After(dayStart) {
			rows = append(rows, makeRow(e, dayStart, dayEnd, cfg.Use24h()))
		}
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].AllDay != rows[b].AllDay {
			return rows[a].AllDay
		}
		if !rows[a].Start.Equal(rows[b].Start) {
			return rows[a].Start.Before(rows[b].Start)
		}
		return rows[a].Title < rows[b].Title
	})
	return rows
}

func buildDays(events []Event, now time.Time, cfg *Config) []Day {
	now = now.In(cfg.Loc)
	today := midnight(now, cfg.Loc)
	var days []Day
	listed := 0
	for i := 0; i < cfg.CalendarLookahead; i++ {
		if i >= cfg.CalendarDays && listed >= minUpcoming {
			break
		}
		dayStart := today.AddDate(0, 0, i)
		rows := rowsForDay(events, dayStart, cfg)
		if len(rows) == 0 && i != 0 {
			continue
		}
		label := dayStart.Format("Monday")
		switch i {
		case 0:
			label = "Today"
		case 1:
			label = "Tomorrow"
		}
		listed += len(rows)
		days = append(days, Day{Label: label, Sub: dayStart.Format("Mon 02 Jan"), Date: dayStart, Rows: rows})
	}
	return days
}

// buildMonthDays lists the days of a month that have something on, for the month overview.
func buildMonthDays(events []Event, month time.Time, cfg *Config) []Day {
	var days []Day
	for d := month; d.Month() == month.Month(); d = d.AddDate(0, 0, 1) {
		if rows := rowsForDay(events, d, cfg); len(rows) > 0 {
			days = append(days, Day{Label: d.Format("Monday"), Sub: d.Format("Mon 02 Jan"), Date: d, Rows: rows})
		}
	}
	return days
}

func makeRow(e Event, dayStart, dayEnd time.Time, h24 bool) Row {
	var when string
	switch {
	case e.AllDay:
		when = "All day"
	case !e.Start.Before(dayStart) && e.End.After(e.Start) && !e.End.After(dayEnd):
		when = clockText(e.Start, h24) + "–" + clockText(e.End, h24)
	case !e.Start.Before(dayStart) && e.End.After(dayEnd):
		when = clockText(e.Start, h24) + "→"
	case !e.Start.Before(dayStart):
		when = clockText(e.Start, h24)
	case !e.End.After(dayEnd):
		when = "until " + clockText(e.End, h24)
	default:
		when = "All day"
	}
	title := e.Title
	if title == "" {
		title = "(No title)"
	}
	return Row{Title: title, Location: e.Location, Cal: e.Cal, When: when, Desc: e.Desc,
		Start: e.Start, End: e.End, AllDay: e.AllDay}
}

func clockText(t time.Time, h24 bool) string {
	if h24 {
		return t.Format("15:04")
	}
	return strings.ToLower(strings.TrimPrefix(t.Format("03:04PM"), "0"))
}
