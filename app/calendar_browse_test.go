package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCleanDescription(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"escapes":    {`Line one\nLine two\, with comma\; and \\ slash`, "Line one\nLine two, with comma; and \\ slash"},
		"html":       {`<p>Hello <b>there</b></p><p>Second &amp; last</p>`, "Hello there\nSecond & last"},
		"breaks":     {`One<br>Two<BR/>Three`, "One\nTwo\nThree"},
		"blank runs": {"A\n\n\n\nB\r\n  \n C ", "A\n\nB\n\nC"},
		"empty":      {"", ""},
		"only tags":  {"<div></div>", ""},
	} {
		if got := cleanDescription(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
	long := cleanDescription(strings.Repeat("word ", 2000))
	if n := len([]rune(long)); n > maxDescription+1 || !strings.HasSuffix(long, "…") {
		t.Errorf("a long description should be cut with an ellipsis, got %d runes", n)
	}
}

func TestBrowseWindowAndDescriptions(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	cfg := &Config{Loc: loc, CalendarDays: 7, CalendarLookahead: 60}
	cfg.applyDefaults()
	now := time.Date(2026, 11, 25, 11, 0, 0, 0, loc)
	start, end := browseWindow(cfg, now)
	if !start.Equal(time.Date(2025, 11, 1, 0, 0, 0, 0, loc)) || !end.Equal(time.Date(2028, 12, 1, 0, 0, 0, 0, loc)) {
		t.Fatalf("window %v .. %v", start, end)
	}
	day := func(months int, d int) string {
		return now.AddDate(0, months, 0).AddDate(0, 0, d-now.Day()).Format("20060102")
	}
	ics := "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:x\n" +
		"BEGIN:VEVENT\nUID:a\nSUMMARY:Next year\nDTSTART:" + day(14, 10) + "T090000\nDTEND:" + day(14, 10) + "T100000\nDESCRIPTION:Bring the form<br>and ID\\, please\nEND:VEVENT\n" +
		"BEGIN:VEVENT\nUID:b\nSUMMARY:Eleven months ago\nDTSTART:" + day(-11, 10) + "T090000\nEND:VEVENT\n" +
		"BEGIN:VEVENT\nUID:c\nSUMMARY:Too far back\nDTSTART:" + day(-14, 10) + "T090000\nEND:VEVENT\n" +
		"BEGIN:VEVENT\nUID:d\nSUMMARY:Too far ahead\nDTSTART:" + day(40, 10) + "T090000\nEND:VEVENT\n" +
		"BEGIN:VEVENT\nUID:e\nSUMMARY:Every week\nDTSTART:" + day(0, 1) + "T180000\nRRULE:FREQ=WEEKLY;COUNT=300\nEND:VEVENT\n" +
		"END:VCALENDAR\n"
	evs, err := eventsFromICS([]byte(ics), "Cal", start, end, loc)
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]int{}
	var desc string
	for _, e := range evs {
		titles[e.Title]++
		if e.Title == "Next year" {
			desc = e.Desc
		}
	}
	if titles["Next year"] != 1 || titles["Eleven months ago"] != 1 || titles["Too far back"] != 0 || titles["Too far ahead"] != 0 {
		t.Errorf("window membership: %v", titles)
	}
	if titles["Every week"] < 100 {
		t.Errorf("repeats should be expanded across the whole window, got %d", titles["Every week"])
	}
	if desc != "Bring the form\nand ID, please" {
		t.Errorf("description %q", desc)
	}
	// the agenda's own look-ahead still wins if it is longer than the browse window
	cfg2 := &Config{Loc: loc, CalendarBrowseBack: 1, CalendarBrowseAhead: 1, CalendarLookahead: 400}
	if _, e2 := browseWindow(cfg2, now); e2.Before(now.AddDate(0, 0, 400)) {
		t.Errorf("window should cover the look-ahead: %v", e2)
	}
}

func TestMonthDaysAndDayRows(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	cfg := &Config{Loc: loc, CalendarDays: 7, CalendarLookahead: 60}
	cfg.applyDefaults()
	d := func(m, day, h int) time.Time { return time.Date(2027, time.Month(m), day, h, 0, 0, 0, loc) }
	events := []Event{
		{Title: "Late", Start: d(3, 5, 18), End: d(3, 5, 19), Cal: "A"},
		{Title: "Early", Start: d(3, 5, 8), End: d(3, 5, 9), Cal: "A"},
		{Title: "Trip", Start: d(3, 7, 0), End: d(3, 10, 0), AllDay: true, Cal: "B"},
		{Title: "Other month", Start: d(4, 2, 9), End: d(4, 2, 10)},
	}
	days := buildMonthDays(events, d(3, 1, 0), cfg)
	var got []string
	for _, day := range days {
		var titles []string
		for _, r := range day.Rows {
			titles = append(titles, r.Title)
		}
		got = append(got, day.Sub+": "+strings.Join(titles, ","))
	}
	want := []string{"Fri 05 Mar: Early,Late", "Sun 07 Mar: Trip", "Mon 08 Mar: Trip", "Tue 09 Mar: Trip"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("month days:\n got %v\nwant %v", got, want)
	}
	if rows := rowsForDay(events, d(3, 8, 0), cfg); len(rows) != 1 || rows[0].When != "All day" {
		t.Errorf("a multi-day event is listed on each of its days: %+v", rows)
	}
}

func TestDetailWhen(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	at := func(d, h, m int) time.Time { return time.Date(2027, 3, d, h, m, 0, 0, loc) }
	for name, c := range map[string]struct {
		row         Row
		date, clock string
	}{
		"timed":     {Row{Start: at(5, 9, 0), End: at(5, 10, 30)}, "Friday 5 March 2027", "09:00 – 10:30   (1 h 30 min)"},
		"short":     {Row{Start: at(5, 9, 0), End: at(5, 9, 45)}, "Friday 5 March 2027", "09:00 – 09:45   (45 min)"},
		"hour":      {Row{Start: at(5, 9, 0), End: at(5, 10, 0)}, "Friday 5 March 2027", "09:00 – 10:00   (1 h)"},
		"no end":    {Row{Start: at(5, 9, 0), End: at(5, 9, 0)}, "Friday 5 March 2027", "09:00"},
		"all day":   {Row{Start: at(5, 0, 0), End: at(6, 0, 0), AllDay: true}, "Friday 5 March 2027", "All day"},
		"multiday":  {Row{Start: at(7, 0, 0), End: at(10, 0, 0), AllDay: true}, "Sun 7 Mar  –  Tue 9 Mar 2027", "All day"},
		"overnight": {Row{Start: at(5, 22, 0), End: at(6, 2, 0)}, "Fri 5 Mar 22:00  →  Sat 6 Mar 02:00", ""},
	} {
		date, clock := detailWhen(c.row, true)
		if date != c.date || clock != c.clock {
			t.Errorf("%s: %q / %q, want %q / %q", name, date, clock, c.date, c.clock)
		}
	}
}

// --- the screen, tapped ------------------------------------------------------------------

func calApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t, t.TempDir())
	a.view = ViewCalendar
	a.cal.Refresh(time.Now())
	a.draw(true)
	return a
}

// tapAction taps the middle of the zone with this action, as a finger would.
func tapAction(t *testing.T, a *App, action string) {
	t.Helper()
	var zone HitZone
	found := false
	a.mu.Lock()
	for _, z := range a.hits {
		if z.Action == action {
			zone, found = z, true
		}
	}
	a.mu.Unlock()
	if !found {
		t.Fatalf("nothing to tap for %q on this screen", action)
	}
	x, y := LogicalToDevice(zone.R.CX(), zone.R.CY(), a.orient)
	a.lastTap = time.Time{}
	a.handle(InputEvent{Kind: "tap", X: x, Y: y})
}

func hasAction(a *App, action string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, z := range a.hits {
		if z.Action == action {
			return true
		}
	}
	return false
}

func TestBrowsingMonthsAndDays(t *testing.T) {
	for _, o := range []Orient{Portrait, LandscapeCW} {
		a := calApp(t)
		a.orient = o
		a.draw(true)
		loc := a.cfg.Loc
		now := time.Now().In(loc)
		thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)

		// every day of the month can be tapped, and the controls are there
		days := thisMonth.AddDate(0, 1, -1).Day()
		n := 0
		a.mu.Lock()
		for _, z := range a.hits {
			if strings.HasPrefix(z.Action, "cal_day:") {
				n++
			}
		}
		a.mu.Unlock()
		if n != days || !hasAction(a, "cal_prev") || !hasAction(a, "cal_next") {
			t.Fatalf("%s: %d day zones for %d days; controls prev=%v next=%v", o, n, days, hasAction(a, "cal_prev"), hasAction(a, "cal_next"))
		}
		if hasAction(a, "cal_today") {
			t.Error("Today does nothing when already on today, so it should not be tappable")
		}

		// forward a month: the month's events are listed and tappable
		tapAction(t, a, "cal_next")
		next := thisMonth.AddDate(0, 1, 0)
		if a.calv.Off != 1 || !a.calv.Day.IsZero() || len(a.calRows) == 0 || !hasAction(a, "cal_today") {
			t.Fatalf("%s: after next: %+v with %d rows", o, a.calv, len(a.calRows))
		}

		// pick a day that has something on, then step through days
		var busyDay time.Time
		for d := next; d.Month() == next.Month(); d = d.AddDate(0, 0, 1) {
			if len(rowsForDay(a.cal.Agenda(now).Events, d, a.cfg)) > 0 {
				busyDay = d
				break
			}
		}
		if busyDay.IsZero() {
			t.Fatal("the demo has nothing next month")
		}
		tapAction(t, a, "cal_day:"+busyDay.Format("20060102"))
		want := rowsForDay(a.cal.Agenda(now).Events, busyDay, a.cfg)
		if !a.calv.Day.Equal(busyDay) || len(a.calRows) != len(want) {
			t.Fatalf("%s: day panel lists %d rows, want %d (day %v)", o, len(a.calRows), len(want), a.calv.Day)
		}
		tapAction(t, a, "cal_dnext")
		if !a.calv.Day.Equal(busyDay.AddDate(0, 0, 1)) {
			t.Errorf("%s: next day is %v", o, a.calv.Day)
		}
		tapAction(t, a, "cal_dprev")
		tapAction(t, a, "cal_dprev")
		if !a.calv.Day.Equal(busyDay.AddDate(0, 0, -1)) {
			t.Errorf("%s: previous day is %v", o, a.calv.Day)
		}

		// stepping past the end of a month moves to the next month, and back
		last := next.AddDate(0, 1, -1)
		a.calv.Day = last
		a.draw(false)
		tapAction(t, a, "cal_dnext")
		if a.calv.Off != 2 || !a.calv.Day.Equal(last.AddDate(0, 0, 1)) {
			t.Errorf("%s: crossing into the next month: %+v", o, a.calv)
		}
		tapAction(t, a, "cal_dprev")
		if a.calv.Off != 1 || !a.calv.Day.Equal(last) {
			t.Errorf("%s: crossing back: %+v", o, a.calv)
		}

		// back to today restores the plain agenda
		tapAction(t, a, "cal_today")
		if a.calv.Off != 0 || !a.calv.Day.IsZero() || hasAction(a, "cal_today") {
			t.Errorf("%s: Today: %+v", o, a.calv)
		}
		// and back a month works too
		tapAction(t, a, "cal_prev")
		if a.calv.Off != -1 {
			t.Errorf("%s: previous month: %+v", o, a.calv)
		}
	}
}

func TestBrowsingStopsAtTheEdges(t *testing.T) {
	a := calApp(t)
	a.calv.Off = a.cfg.CalendarBrowseAhead
	a.draw(false)
	if hasAction(a, "cal_next") || !hasAction(a, "cal_prev") {
		t.Error("no way forward past the last browsable month")
	}
	a.calv.Off = -a.cfg.CalendarBrowseBack
	a.draw(false)
	if hasAction(a, "cal_prev") || !hasAction(a, "cal_next") {
		t.Error("no way back past the first browsable month")
	}
	// the day stepper stops at the edge too
	loc := a.cfg.Loc
	now := time.Now().In(loc)
	lastMonth := time.Date(now.Year(), now.Month()+time.Month(a.cfg.CalendarBrowseAhead), 1, 0, 0, 0, 0, loc)
	a.calv = CalView{Off: a.cfg.CalendarBrowseAhead, Day: lastMonth.AddDate(0, 1, -1)}
	a.draw(false)
	if hasAction(a, "cal_dnext") {
		t.Error("the day stepper must not leave the browsable range")
	}
}

func TestOpeningAnEventAndPaging(t *testing.T) {
	a := calApp(t)
	// today's agenda: the first row opens
	if len(a.calRows) == 0 {
		t.Fatal("demo has events today")
	}
	first := a.calRows[0]
	tapAction(t, a, "cal_ev:0")
	if a.calv.Detail == nil || a.calv.Detail.Title != first.Title {
		t.Fatalf("detail: %+v", a.calv.Detail)
	}
	if hasAction(a, "cal_next") || !hasAction(a, "cal_back") {
		t.Error("the detail view replaces the month; only Back is offered")
	}
	tapAction(t, a, "cal_back")
	if a.calv.Detail != nil || !hasAction(a, "cal_next") {
		t.Error("Back returns to the calendar")
	}

	// a month list with several pages
	tapAction(t, a, "cal_next")
	if a.calPages < 2 {
		t.Fatalf("demo December should need several pages, got %d", a.calPages)
	}
	page0 := a.calRows[0]
	tapAction(t, a, "cal_page_next")
	if a.calv.Page != 1 || a.calRows[0].Title == page0.Title && a.calRows[0].Start.Equal(page0.Start) {
		t.Errorf("next page: page %d, first row %v", a.calv.Page, a.calRows[0].Start)
	}
	tapAction(t, a, "cal_page_prev")
	if a.calv.Page != 0 || !a.calRows[0].Start.Equal(page0.Start) {
		t.Errorf("previous page: page %d", a.calv.Page)
	}
	if hasAction(a, "cal_page_prev") {
		t.Error("no previous page from the first page")
	}
	// opening a row from a later page opens that very event
	tapAction(t, a, "cal_page_next")
	second := a.calRows[1]
	tapAction(t, a, "cal_ev:1")
	if a.calv.Detail == nil || a.calv.Detail.Title != second.Title || !a.calv.Detail.Start.Equal(second.Start) {
		t.Errorf("opened %+v, tapped %+v", a.calv.Detail, second)
	}
	tapAction(t, a, "cal_back")
	if a.calv.Page != 1 {
		t.Errorf("Back should return to the page you were on, got %d", a.calv.Page)
	}

	// a long description pages too
	var long []string
	for i := 0; i < 40; i++ {
		long = append(long, fmt.Sprintf("Paragraph %d of the invented description, long enough to wrap onto a second line when the column is narrow, and then some.", i))
	}
	row := first
	row.Desc = strings.Join(long, "\n\n")
	a.calv = CalView{Detail: &row}
	a.draw(false)
	if a.calDPages < 2 || !hasAction(a, "cal_dpage_next") || hasAction(a, "cal_dpage_prev") {
		t.Fatalf("description pages: %d", a.calDPages)
	}
	tapAction(t, a, "cal_dpage_next")
	if a.calv.DPage != 1 || !hasAction(a, "cal_dpage_prev") {
		t.Errorf("description page %d", a.calv.DPage)
	}
	a.calv.DPage = 99 // out of range is pulled back, not drawn blank
	a.draw(false)
	if a.calv.DPage != a.calDPages-1 {
		t.Errorf("page clamped to %d, want %d", a.calv.DPage, a.calDPages-1)
	}
}

// Every state draws in both orientations without trouble and offers the taps it promises.
func TestCalendarStatesRender(t *testing.T) {
	a := calApp(t)
	now := time.Now().In(a.cfg.Loc)
	ev := a.calRows[0]
	states := map[string]CalView{
		"agenda": {}, "day": {Day: midnight(now, a.cfg.Loc)}, "empty day": {Day: midnight(now, a.cfg.Loc).AddDate(0, 0, 200)},
		"month": {Off: 1}, "empty month": {Off: 20}, "past": {Off: -3}, "event": {Detail: &ev},
		"no description": {Detail: &Row{Title: "Plain", Start: now, End: now.Add(time.Hour)}},
	}
	for name, v := range states {
		for _, o := range []Orient{Portrait, LandscapeCW} {
			a.orient, a.calv = o, v
			a.draw(true) // would panic on a layout mistake
			if v.Detail == nil && !hasAction(a, "cal_prev") {
				t.Errorf("%s/%s: no month controls", name, o)
			}
			rowZones := 0
			a.mu.Lock()
			for _, z := range a.hits {
				if strings.HasPrefix(z.Action, "cal_ev:") {
					rowZones++
				}
			}
			a.mu.Unlock()
			if rowZones != len(a.calRows) {
				t.Errorf("%s/%s: %d row zones for %d listed rows", name, o, rowZones, len(a.calRows))
			}
		}
	}
}
