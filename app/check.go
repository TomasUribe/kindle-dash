package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// runCheck prints what this device can and cannot do, without touching the screen.
// The Kindle's "diagnose" launcher runs it and saves the output to a file.
func runCheck(cfg *Config, fbinkPath, touchDev string) {
	fmt.Println("kdash self-check", time.Now().Format(time.RFC3339))
	fmt.Printf("program: %s/%s, go %s, input event size %d\n", runtime.GOOS, runtime.GOARCH, runtime.Version(), evSize)
	fmt.Printf("user id: %d\n", os.Getuid())
	fmt.Printf("time zone %s, local time now %s\n", cfg.Timezone, time.Now().In(cfg.Loc).Format("Mon 02 Jan 15:04"))
	fmt.Printf("SSL_CERT_FILE=%q\n", os.Getenv("SSL_CERT_FILE"))

	fmt.Println("\n-- screen")
	if bin := FindFBInk(fbinkPath); bin == "" {
		fmt.Println("fbink: NOT FOUND (needed to draw)")
	} else {
		fmt.Println("fbink:", bin)
		if out, err := exec.Command(bin, "--version").CombinedOutput(); err == nil {
			fmt.Println("  version:", strings.TrimSpace(string(out)))
		}
		if out, err := exec.Command(bin, "-e").CombinedOutput(); err == nil {
			fmt.Println("  screen info:", strings.Join(strings.Fields(string(out)), " "))
		}
	}

	fmt.Println("\n-- input")
	devs := listInputDevices()
	if len(devs) == 0 {
		fmt.Println("no /proc/bus/input/devices (normal on a computer)")
	}
	for _, d := range devs {
		role := ""
		switch {
		case touchName.MatchString(d.name):
			role = "  <- touchscreen candidate"
		case powerName.MatchString(d.name):
			role = "  <- power key candidate"
		}
		fmt.Printf("%s %q%s\n", d.event, d.name, role)
	}
	if touchDev != "" {
		fmt.Println("touch override:", touchDev)
	}

	fmt.Println("\n-- network (DNS and HTTPS, no logins)")
	for _, host := range []string{"calendar.google.com", "accounts.spotify.com", "api.spotify.com", "i.scdn.co", "lrclib.net"} {
		start := time.Now()
		addrs, err := net.LookupHost(host)
		if err != nil {
			fmt.Printf("%-22s DNS FAILED: %v\n", host, err)
			continue
		}
		client := &http.Client{Timeout: 20 * time.Second}
		resp, err := client.Head("https://" + host + "/")
		if err != nil {
			fmt.Printf("%-22s DNS ok (%s), HTTPS FAILED: %s\n", host, addrs[0], safeErr(err))
			continue
		}
		resp.Body.Close()
		fmt.Printf("%-22s ok: HTTP %d in %v\n", host, resp.StatusCode, time.Since(start).Round(time.Millisecond))
	}

	fmt.Println("\n-- calendars")
	if len(cfg.Calendars) == 0 {
		fmt.Println("none configured")
	}
	now := time.Now().In(cfg.Loc)
	winStart := midnight(now, cfg.Loc).AddDate(0, 0, -1)
	winEnd := winStart.AddDate(0, 0, cfg.CalendarLookahead+2)
	for i, spec := range cfg.Calendars {
		name := spec.Name
		if name == "" {
			name = fmt.Sprintf("calendar %d", i+1)
		}
		start := time.Now()
		data, err := fetchICS(spec.URL)
		if err != nil {
			fmt.Printf("%-26s FAILED: %s\n", name, safeErr(err))
			continue
		}
		evs, err := eventsFromICS(data, name, winStart, winEnd, cfg.Loc)
		if err != nil {
			fmt.Printf("%-26s parse FAILED: %s\n", name, safeErr(err))
			continue
		}
		fmt.Printf("%-26s ok: %6d bytes, %3d events in the next %d days (%v)\n", name, len(data), len(evs), cfg.CalendarLookahead, time.Since(start).Round(time.Millisecond))
	}

	fmt.Println("\n-- spotify")
	sp := NewSpotify(cfg)
	switch {
	case cfg.SpotifyClientID == "":
		fmt.Println("no client id configured")
	case !sp.Available():
		fmt.Println("no token file at", cfg.SpotifyTokenFile)
	default:
		p := sp.State(time.Now())
		switch {
		case p.Err != "":
			fmt.Println("FAILED:", p.Err)
		case p.Active:
			fmt.Printf("ok: device %q, %q by %q, playing=%v volume=%d\n", p.Device, p.Title, p.Artist, p.Playing, p.Volume)
		default:
			fmt.Println("ok: logged in, no active device right now")
		}
		if p.Err == "" {
			if items, err := sp.Playlists(); err != nil {
				fmt.Println("library access: FAILED:", err)
			} else {
				fmt.Printf("library access: ok (%d playlists)\n", len(items))
			}
			if p.Active && p.Title != "" {
				if l, err := FetchLyrics(p.Title, p.Artist, p.Album, p.Dur); err != nil {
					fmt.Println("lyrics lookup: FAILED:", err)
				} else {
					fmt.Printf("lyrics lookup for the playing track: found=%v synced=%v instrumental=%v lines=%d\n", l.Found, l.Synced, l.Instrumental, len(l.Lines))
				}
			}
		}
	}
	fmt.Println("\nself-check finished")
}
