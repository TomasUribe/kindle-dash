package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// sampleICS holds the tricky cases: repeats with an excluded day, an edited instance,
// cancelled and overnight events, UTC / zoned / floating times, DURATION and escapes.
func sampleICS(today time.Time) string {
	day := func(n int) string { return today.AddDate(0, 0, n).Format("20060102") }
	local := func(n, h, m int) string { return fmt.Sprintf("%sT%02d%02d00", day(n), h, m) }
	utc := func(n, h int) string { return fmt.Sprintf("%sT%02d0000Z", day(n), h) }
	zoned := func(prop string, n, h, m int) string {
		return fmt.Sprintf("%s;TZID=Asia/Kolkata:%s", prop, local(n, h, m))
	}

	events := []string{
		"SUMMARY:Team sync\nLOCATION:12 Example Street\\, Springfield\n" + zoned("DTSTART", 0, 15, 0) + "\n" + zoned("DTEND", 0, 16, 0),
		"SUMMARY:UTC call\nDTSTART:" + utc(1, 14) + "\nDTEND:" + utc(1, 15),
		"SUMMARY:Alex's birthday\nDTSTART;VALUE=DATE:" + day(1) + "\nDTEND;VALUE=DATE:" + day(2),
		"SUMMARY:Conference trip\nDTSTART;VALUE=DATE:" + day(2) + "\nDTEND;VALUE=DATE:" + day(5),
		"SUMMARY:Daily standup\nUID:daily@test\n" + zoned("DTSTART", 0, 8, 0) + "\n" + zoned("DTEND", 0, 8, 30) +
			"\nRRULE:FREQ=DAILY;COUNT=10\n" + zoned("EXDATE", 1, 8, 0),
		"SUMMARY:Daily standup (moved)\nUID:daily@test\n" + zoned("RECURRENCE-ID", 2, 8, 0) + "\n" +
			zoned("DTSTART", 2, 9, 0) + "\n" + zoned("DTEND", 2, 9, 30),
		"SUMMARY:Cancelled meeting\nSTATUS:CANCELLED\n" + zoned("DTSTART", 0, 11, 0) + "\n" + zoned("DTEND", 0, 12, 0),
		"SUMMARY:Workshop (duration)\n" + zoned("DTSTART", 0, 17, 0) + "\nDURATION:PT1H30M",
		"SUMMARY:Floating dinner\nDTSTART:" + local(0, 19, 0) + "\nDTEND:" + local(0, 21, 0),
		"SUMMARY:Night shift\n" + zoned("DTSTART", 0, 22, 0) + "\n" + zoned("DTEND", 1, 1, 0),
		zoned("DTSTART", 3, 10, 0) + "\n" + zoned("DTEND", 3, 11, 0),
		"SUMMARY:Lunch\\, with Sam\n" + zoned("DTSTART", 3, 13, 0) + "\n" + zoned("DTEND", 3, 14, 0),
		"SUMMARY:Too far away\n" + zoned("DTSTART", 70, 10, 0) + "\n" + zoned("DTEND", 70, 11, 0),
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//kindle-dash test//EN\n")
	for i, e := range events {
		uid := fmt.Sprintf("UID:e%d@test\n", i)
		if strings.Contains(e, "UID:") {
			uid = ""
		}
		b.WriteString("BEGIN:VEVENT\n" + uid + "DTSTAMP:20260101T000000Z\n" + e + "\nEND:VEVENT\n")
	}
	b.WriteString("END:VCALENDAR\n")
	return strings.ReplaceAll(b.String(), "\n", "\r\n")
}

func TestAgendaCases(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Date(2026, 11, 25, 11, 0, 0, 0, loc)
	today := midnight(now, loc)
	cfg := &Config{Loc: loc, CalendarDays: 7, CalendarLookahead: 60}
	events, err := eventsFromICS([]byte(sampleICS(today)), "Test", today.AddDate(0, 0, -1), today.AddDate(0, 0, 62), loc)
	if err != nil {
		t.Fatal(err)
	}
	days := buildDays(events, now, cfg)

	got := map[string][]string{}
	for _, d := range days {
		for _, r := range d.Rows {
			got[d.Label] = append(got[d.Label], r.When+"|"+r.Title)
		}
	}
	check := func(label string, want ...string) {
		t.Helper()
		if strings.Join(got[label], "\n") != strings.Join(want, "\n") {
			t.Errorf("%s:\n got  %q\n want %q", label, got[label], want)
		}
	}
	check("Today", "08:00\u201308:30|Daily standup", "15:00\u201316:00|Team sync", "17:00\u201318:30|Workshop (duration)",
		"19:00\u201321:00|Floating dinner", "22:00\u2192|Night shift")
	check("Tomorrow", "All day|Alex's birthday", "until 01:00|Night shift", "19:30\u201320:30|UTC call")
	check("Friday", "All day|Conference trip", "09:00\u201309:30|Daily standup (moved)")
	check("Saturday", "All day|Conference trip", "08:00\u201308:30|Daily standup", "10:00\u201311:00|(No title)", "13:00\u201314:00|Lunch, with Sam")
	check("Sunday", "All day|Conference trip", "08:00\u201308:30|Daily standup")
	check("Monday", "08:00\u201308:30|Daily standup")

	for _, d := range days {
		for _, r := range d.Rows {
			if strings.Contains(r.Title, "Cancelled") || strings.Contains(r.Title, "Too far") {
				t.Errorf("should be hidden: %s", r.Title)
			}
		}
	}
	if days[0].Rows[1].Location != "12 Example Street, Springfield" {
		t.Errorf("location not unescaped: %q", days[0].Rows[1].Location)
	}
}

func TestQuietWeekLooksAhead(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Date(2026, 11, 25, 11, 0, 0, 0, loc)
	var evs []Event
	for i := 0; i < 8; i++ {
		s := midnight(now, loc).AddDate(0, 0, 10+i)
		evs = append(evs, Event{Title: fmt.Sprintf("far %d", i), Start: s.Add(9 * time.Hour), End: s.Add(10 * time.Hour)})
	}
	days := buildDays(evs, now, &Config{Loc: loc, CalendarDays: 7, CalendarLookahead: 60})
	if len(days) < 7 || days[0].Label != "Today" || len(days[0].Rows) != 0 {
		t.Fatalf("expected an empty Today then upcoming days, got %d days", len(days))
	}
	total := 0
	for _, d := range days {
		total += len(d.Rows)
	}
	if total < minUpcoming {
		t.Errorf("should list at least %d upcoming events, got %d", minUpcoming, total)
	}
}

func TestDuration(t *testing.T) {
	cases := map[string]time.Duration{"PT1H30M": 90 * time.Minute, "P1D": 24 * time.Hour, "P2W": 14 * 24 * time.Hour, "PT45S": 45 * time.Second}
	for in, want := range cases {
		if got, ok := parseDuration(in); !ok || got != want {
			t.Errorf("%s: got %v ok=%v want %v", in, got, ok, want)
		}
	}
}
