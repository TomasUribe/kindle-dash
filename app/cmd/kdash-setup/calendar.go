package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// normalizeCalendarURL accepts what people paste: webcal:// addresses, stray quotes and spaces.
func normalizeCalendarURL(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"'<>`)
	if rest, ok := strings.CutPrefix(s, "webcal://"); ok {
		s = "https://" + rest
	}
	return s
}

var checkClient = &http.Client{Timeout: 25 * time.Second}

// checkCalendar downloads an iCal address and says how many events it holds and what the calendar calls
// itself. Errors never contain the address, which is a secret.
func checkCalendar(addr string) (events int, name string, err error) {
	u, perr := url.Parse(addr)
	if perr != nil || u.Scheme != "https" || u.Host == "" {
		return 0, "", errors.New("that does not look like an https:// address")
	}
	resp, gerr := checkClient.Get(addr)
	if gerr != nil {
		var ue *url.Error
		if errors.As(gerr, &ue) {
			gerr = ue.Err
		}
		return 0, "", fmt.Errorf("could not download it: %w", gerr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, "", fmt.Errorf("the server answered HTTP %d (is it the *secret* iCal address?)", resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	text := string(body)
	if !strings.Contains(text, "BEGIN:VCALENDAR") {
		return 0, "", errors.New("that address does not return a calendar (iCal) file")
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if v, ok := strings.CutPrefix(line, "X-WR-CALNAME:"); ok && name == "" {
			name = strings.TrimSpace(v)
		}
	}
	return strings.Count(text, "BEGIN:VEVENT"), name, nil
}

// detectTimezone guesses the computer's IANA time zone name, or "" if it cannot tell.
func detectTimezone() string {
	valid := func(tz string) bool { _, err := time.LoadLocation(tz); return tz != "" && tz != "Local" && err == nil }
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); valid(tz) {
		return tz
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); valid(tz) {
			return tz
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 && valid(link[i+len("zoneinfo/"):]) {
			return link[i+len("zoneinfo/"):]
		}
	}
	return ""
}
