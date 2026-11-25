package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

func main() {
	demo := flag.Bool("demo", false, "use fake calendar and Spotify data")
	renderDir := flag.String("render", "", "write a PNG of every view and orientation to this folder and exit")
	simAddr := flag.String("sim", "", "run in a browser simulator at this address, e.g. 127.0.0.1:8790")
	configPath := flag.String("config", "", "path to config.json (default: next to the program)")
	fbinkPath := flag.String("fbink", "", "path to fbink (default: search the usual places)")
	touchDev := flag.String("touch", "", "touchscreen device, e.g. /dev/input/event1 (default: find it)")
	logPath := flag.String("log", "", "write the log here instead of the terminal")
	check := flag.Bool("check", false, "test network, calendars, Spotify, fbink and input, then exit")
	iconPath := flag.String("icon", "", "write the launcher cover image (PNG) to this file and exit")
	flag.Parse()

	logOut := io.Writer(os.Stderr)
	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err == nil {
			defer f.Close()
			logOut = f
		}
	}
	lg := log.New(logOut, "", log.Ltime)
	logf := func(format string, args ...any) { lg.Printf(format, args...) }

	cfg := DemoConfig()
	haveConfig := false
	if *configPath == "" {
		if exe, err := os.Executable(); err == nil {
			if p := filepath.Join(filepath.Dir(exe), "config.json"); fileExists(p) {
				*configPath = p
			}
		}
	}
	if *configPath != "" {
		var err error
		if cfg, err = LoadConfig(*configPath); err != nil {
			fmt.Fprintln(os.Stderr, "config:", err)
			os.Exit(1)
		}
		haveConfig = true
	}
	useDemo := *demo || !haveConfig

	switch {
	case *iconPath != "":
		if err := writeIcon(*iconPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case *check:
		runCheck(cfg, *fbinkPath, *touchDev)
	case *renderDir != "":
		if err := renderAll(*renderDir, cfg, useDemo); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case *simAddr != "":
		runSim(*simAddr, cfg, useDemo, logf)
	default:
		runDevice(cfg, useDemo, *fbinkPath, *touchDev, logf)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func runSim(addr string, cfg *Config, demo bool, logf func(string, ...any)) {
	input := make(chan InputEvent, 16)
	nd := &NullDisplay{}
	app := NewApp(cfg, demo, nd, input, logf)
	go func() {
		if err := ServeSim(addr, app, input, nd, logf); err != nil {
			logf("simulator: %v", err)
			os.Exit(1)
		}
	}()
	app.Run()
}

func runDevice(cfg *Config, demo bool, fbinkPath, touchDev string, logf func(string, ...any)) {
	bin := FindFBInk(fbinkPath)
	if bin == "" {
		logf("fbink not found: this program draws with fbink, which the jailbreak installs")
		os.Exit(2)
	}
	tmp := filepath.Join(os.TempDir(), "kdash")
	_ = os.MkdirAll(tmp, 0o755)
	logf("kdash starting: fbink=%s demo=%v calendars=%d tz=%s", bin, demo, len(cfg.Calendars), cfg.Timezone)

	input := make(chan InputEvent, 16)
	if err := StartInput(touchDev, input, logf); err != nil {
		logf("input: %v", err)
		os.Exit(3)
	}
	app := NewApp(cfg, demo, &FBInk{Bin: bin, Dir: tmp, FullArgs: cfg.FbinkFullArgs, PartialArgs: cfg.FbinkPartialArgs}, input, logf)
	app.Run()
	logf("kdash exiting")
}

// renderAll draws each view in each orientation, for checking the design on a computer.
func renderAll(dir string, cfg *Config, demo bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	now := time.Now().In(cfg.Loc)
	cal := NewCalendarSource(cfg, demo)
	cal.Refresh(now)
	var player Player
	if demo {
		player = newDemoPlayer(now).State(now)
	} else {
		player = NewSpotify(cfg).State(now)
	}
	art := NewArtCache()
	if player.ArtURL != "" {
		done := make(chan struct{})
		art.Request(player.ArtURL, func() { close(done) })
		select {
		case <-done:
		case <-time.After(20 * time.Second):
		}
	}
	names := []string{"home", "calendar", "music", "settings", "library", "lyrics"}
	orients := []string{"portrait", "landscape"}
	for vi, vn := range names {
		for oi, on := range orients {
			s := &Scene{Now: now, Cfg: cfg, View: View(vi), Orient: Orient(oi), Agenda: cal.Agenda(now),
				Player: player, Battery: 78, Light: NewFrontLight().Read()}
			if demo {
				dp := newDemoPlayer(now)
				s.Library = LibView{Section: LibPlaylists, List: LibList{Items: dp.Library(LibPlaylists), Loaded: true}}
				s.Lyrics = LyricsView{L: demoLyrics(), Auto: true, Size: lyricSizes[1], Highlight: true}
			}
			s.ArtAt = func(size int) *image.Gray { return art.Get(player.ArtURL, size) }
			canvas, _ := Render(s)
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%s.png", vn, on)))
			if err != nil {
				return err
			}
			if err := png.Encode(f, canvas.Img); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	// the calendar's other states: another month, one day, one event
	if demo {
		next := CalView{Off: 1}
		day := CalView{Day: midnight(now, cfg.Loc).AddDate(0, 0, 2)}
		var ev Row
		if rows := rowsForDay(cal.Agenda(now).Events, midnight(now, cfg.Loc), cfg); len(rows) > 1 {
			ev = rows[1]
		}
		detail := CalView{Detail: &ev}
		for name, cv := range map[string]CalView{"calendar-month": next, "calendar-day": day, "calendar-event": detail} {
			for oi, on := range orients {
				s := &Scene{Now: now, Cfg: cfg, View: ViewCalendar, Orient: Orient(oi), Agenda: cal.Agenda(now),
					Player: player, Battery: 78, Light: NewFrontLight().Read(), Cal: cv}
				canvas, _ := Render(s)
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%s.png", name, on)))
				if err != nil {
					return err
				}
				if err := png.Encode(f, canvas.Img); err != nil {
					f.Close()
					return err
				}
				f.Close()
			}
		}
	}
	fmt.Println("wrote", len(names)*len(orients), "images to", dir)
	return nil
}

// writeIcon draws the cover shown for the launcher in the Kindle library.
func writeIcon(path string) error {
	const w, h = 600, 800
	c := NewCanvas(w, h)
	c.Card(Rect{0, 0, w, h}, 0, 0, Black, Black)
	c.Card(Rect{30, 30, w - 60, h - 60}, 36, 6, Black, White)
	IconCalendar(c, 220, 300, 230, White, Black)
	IconMusic(c, 400, 360, 200, White)
	f := Face(Bold, 74)
	c.TextIn("Kindle", Rect{0, 520, w, LineHeight(f)}, f, White, Center)
	c.TextIn("Dash", Rect{0, 520 + LineHeight(f) - 6, w, LineHeight(f)}, f, White, Center)
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, c.Img)
}
