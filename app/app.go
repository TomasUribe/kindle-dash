package main

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// An e-ink panel takes a good fraction of a second to redraw and keeps ghosts of old images,
// so the app avoids needless updates:
//   - several changes that arrive close together become a single redraw (settleDelay);
//   - Spotify is polled slowly, the progress bar moves in steps (Config.ProgressSeconds),
//     and only the parts of the screen that changed are sent to the panel;
//   - every Config.FullRefreshEvery partial updates, or on a screen change, a clean flash clears ghosts.
const settleDelay = 350 * time.Millisecond

// App owns the screen. Everything that changes what is drawn runs on the loop goroutine
// (see post), so the state below needs no locks; only the last frame is shared with the simulator.
type App struct {
	cfg   *Config
	demo  bool
	cal   *CalendarSource
	spot  *Spotify
	dp    *demoPlayer
	art   *ArtCache
	disp  Display
	logf  func(string, ...any)
	light *FrontLight

	input  <-chan InputEvent
	tasks  chan func()
	redraw chan struct{}

	// loop-goroutine state
	view    View
	orient  Orient
	player  Player
	pressed string
	// where the last seek landed, and on which track, so the progress line is drawn from that point
	seekAnchor time.Duration
	seekKey    string
	// the Calendar screen: which month, day and event is showing, and what the last frame listed
	calv                CalView
	calRows             []Row
	calPages, calDPages int
	// the song after the current one (from Spotify's queue), looked up near the end of the current one
	nextTrack Player
	nextFor   string // key of the song nextTrack was looked up for
	nextBusy  bool
	nextTimer *time.Timer
	endTimer  *time.Timer // asks Spotify right when the song should end
	endBusy   atomic.Bool // a "keep asking until it changes" run is going
	early     earlySwap
	status    string
	statusAt  time.Time
	battery   int
	lightSt   LightState
	night     bool
	lastDev   *image.Gray
	partials  int
	lastTap   time.Time

	lib               [libSections]LibList
	libSec            LibSection
	libPage           int
	libRows, libPages int

	lyrCache                    map[string]*Lyrics
	lyrBusy                     map[string]bool
	lyrErr                      map[string]string
	lyrKey                      string
	lyrPage, lyrShown, lyrPages int
	lyrTurns                    []time.Duration // when each page after the first comes up (from the last draw)
	lyrAuto, lyrHL              bool
	lyrSize                     int
	lyrTimer                    *time.Timer

	// mirrored for the background goroutine
	viewNow    atomic.Int32
	playingNow atomic.Bool
	minuteSeen atomic.Int32

	mu         sync.Mutex // guards the fields below, read by the simulator
	last       *image.Gray
	lastOrient Orient
	hits       []HitZone
	version    int
}

func NewApp(cfg *Config, demo bool, disp Display, input <-chan InputEvent, logf func(string, ...any)) *App {
	a := &App{cfg: cfg, demo: demo, disp: disp, input: input, logf: logf,
		cal: NewCalendarSource(cfg, demo), spot: NewSpotify(cfg), art: NewArtCache(), light: NewFrontLight(),
		tasks: make(chan func(), 64), redraw: make(chan struct{}, 1), battery: -1,
		lyrCache: map[string]*Lyrics{}, lyrBusy: map[string]bool{}, lyrErr: map[string]string{},
		lyrAuto: true, lyrSize: lyricSizes[1]}
	a.player = Player{Volume: -1, Hint: "Loading…"}
	if demo {
		a.dp = newDemoPlayer(time.Now())
	}
	a.minuteSeen.Store(-1)
	return a
}

// Run draws the first frame and then reacts to touches, timers and data until told to quit.
func (a *App) Run() {
	a.loadState()
	a.lightSt = a.light.Read()
	a.logf("front light: available=%v level=%d/%d warmth=%v %d/%d", a.lightSt.Available, a.lightSt.Level,
		a.lightSt.Max, a.lightSt.HasWarmth, a.lightSt.Warmth, a.lightSt.WarmMax)
	go a.background()
	a.enterView(a.view)
	a.draw(true)

	timer := time.NewTimer(time.Hour)
	timer.Stop()
	armed := false
	arm := func() {
		if !armed {
			armed = true
			timer.Reset(settleDelay)
		}
	}
	for {
		select {
		case ev := <-a.input:
			if a.handle(ev) {
				return
			}
		case <-a.redraw:
			arm()
		case fn := <-a.tasks:
			fn()
			arm()
		case <-timer.C:
			armed = false
			a.draw(false)
		}
	}
}

func (a *App) signal() {
	select {
	case a.redraw <- struct{}{}:
	default:
	}
}

// post runs fn on the loop goroutine and then redraws (after a short settling delay).
func (a *App) post(fn func()) {
	a.tasks <- fn
}

// --- input -------------------------------------------------------------------

func (a *App) handle(ev InputEvent) (quit bool) {
	switch ev.Kind {
	case "power":
		a.logf("power button: exit")
		return true
	case "swipe":
		x0, y0 := DeviceToLogical(ev.X, ev.Y, a.orient)
		x1, _ := DeviceToLogical(ev.X+ev.DX, ev.Y+ev.DY, a.orient)
		if z, ok := a.hitZone(x0, y0); ok && z.Action == seekAction {
			a.seekTo(seekPermille(z.R, x1)) // dragging along the progress line: jump to where the finger lifted
			return false
		}
		if abs(ev.DX) > 2*abs(ev.DY) || abs(ev.DY) > 2*abs(ev.DX) {
			if dx := x1 - x0; dx < -150 {
				a.setView(stepView(a.view, 1))
			} else if dx > 150 {
				a.setView(stepView(a.view, -1))
			}
		}
		return false
	case "tap":
	default:
		return false
	}
	if time.Since(a.lastTap) < 300*time.Millisecond {
		return false // a double tap would cost two slow screen updates
	}
	a.lastTap = time.Now()

	lx, ly := DeviceToLogical(ev.X, ev.Y, a.orient)
	act := a.hit(lx, ly)
	a.logf("tap device(%d,%d) logical(%d,%d) -> %q", ev.X, ev.Y, lx, ly, act)
	switch {
	case act == "":
	case act == "exit":
		return true
	case act == "rotate":
		a.orient = a.orient.Next()
		a.saveState()
		a.draw(true)
	case act == "refresh":
		a.draw(true)
	case strings.HasPrefix(act, "view:"):
		for _, v := range viewOrder {
			if v.Action() == act {
				a.setView(v)
			}
		}
	case isSettingsAction(act):
		a.settingsAction(act)
	case strings.HasPrefix(act, "lib"):
		a.libraryAction(act)
	case strings.HasPrefix(act, "lyr_"):
		a.lyricsAction(act)
	case strings.HasPrefix(act, "cal_"):
		a.calendarAction(act)
	case strings.HasPrefix(act, "seek:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(act, "seek:"))
		a.seekTo(n)
	default: // a Spotify control
		if a.pressed != "" {
			return false // one command at a time
		}
		a.pressed = act
		if act == "toggle" {
			a.player.Playing = !a.player.Playing
			a.player.FetchedAt = time.Now()
		}
		a.draw(false) // show the pressed button right away
		go a.runAction(act)
	}
	return false
}

func (a *App) setView(v View) {
	if v == a.view {
		return
	}
	a.view = v
	a.saveState()
	a.enterView(v)
	a.draw(true)
}

// seekTo jumps to a point in the song, given as a fraction in per mille. The progress line moves at once
// and Spotify is told in the background.
func (a *App) seekTo(permille int) {
	p := a.player
	if !p.Active || p.Dur <= 0 {
		a.setStatus("Nothing is playing")
		a.draw(false)
		return
	}
	if a.pressed != "" {
		return // one command at a time
	}
	pos := p.Dur * time.Duration(max(0, min(1000, permille))) / 1000
	pos = max(0, min(pos, p.Dur-1500*time.Millisecond)) // the very end would skip to the next song
	a.logf("seek to %v of %v", pos.Round(time.Second), p.Dur.Round(time.Second))
	a.pressed = "seek"
	a.player.Progress, a.player.FetchedAt = pos, time.Now()
	a.seekAnchor, a.seekKey = pos, a.lyricsKeyNow()
	a.lyrAuto = true // lyrics follow the song to the new place
	a.draw(false)
	go a.runAction(fmt.Sprintf("seek:%d", pos.Milliseconds()))
}

// enterView loads whatever the screen needs when it is opened.
func (a *App) enterView(v View) {
	switch v {
	case ViewSettings:
		a.lightSt = a.light.Read() // show what the Kindle really has right now
	case ViewLibrary:
		a.ensureLibrary(a.libSec, false)
	case ViewLyrics:
		a.ensureLyrics()
	}
	if v == ViewHome || v == ViewMusic || v == ViewLyrics {
		go a.poll() // fresh music info when the music is on screen
	}
}

// hitZone is the topmost zone under a point.
func (a *App) hitZone(x, y int) (HitZone, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.hits) - 1; i >= 0; i-- {
		if a.hits[i].R.Contains(x, y) {
			return a.hits[i], true
		}
	}
	return HitZone{}, false
}

// hit is the action under a point. A tap on a progress line becomes "seek:<per mille>".
func (a *App) hit(x, y int) string {
	z, ok := a.hitZone(x, y)
	switch {
	case !ok:
		return ""
	case z.Action == seekAction:
		return fmt.Sprintf("seek:%d", seekPermille(z.R, x))
	}
	return z.Action
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// --- settings --------------------------------------------------------------------------

func isSettingsAction(act string) bool {
	switch {
	case strings.HasPrefix(act, "fl:"), strings.HasPrefix(act, "fl_"), strings.HasPrefix(act, "warm:"), strings.HasPrefix(act, "warm_"):
		return true
	}
	switch act {
	case "night", "clock", "full", "sync":
		return true
	}
	return false
}

// settingsAction handles taps on the Settings screen.
func (a *App) settingsAction(act string) {
	st := a.lightSt
	var err error
	num := func(prefix string) int { n, _ := strconv.Atoi(strings.TrimPrefix(act, prefix)); return n }
	switch {
	case strings.HasPrefix(act, "fl:"):
		err = a.light.SetLevel(num("fl:"))
	case act == "fl_up":
		err = a.light.SetLevel(st.Level + 1)
	case act == "fl_down":
		err = a.light.SetLevel(st.Level - 1)
	case strings.HasPrefix(act, "warm:"):
		err = a.light.SetWarmth(num("warm:"))
	case act == "warm_up":
		err = a.light.SetWarmth(st.Warmth + 1)
	case act == "warm_down":
		err = a.light.SetWarmth(st.Warmth - 1)
	case act == "night":
		a.night = !a.night
		a.saveState()
		a.logf("night mode: %v", a.night)
		a.draw(true)
		return
	case act == "clock":
		v := !a.cfg.Use24h()
		a.cfg.Clock24 = &v
		a.saveState()
	case act == "full":
		a.draw(true)
		return
	case act == "sync":
		a.setStatus("Syncing…")
		go func() {
			a.cal.Refresh(time.Now())
			a.post(func() { a.status = "" })
		}()
		go a.poll()
		for i := range a.lib {
			a.lib[i].Loaded = false // library lists reload next time they are opened
		}
	}
	if err != nil {
		a.logf("light: %s: %v", act, err)
		a.setStatus("Could not change the light: " + err.Error())
	}
	a.lightSt = a.light.Read()
	a.draw(false)
}

// --- music controls ---------------------------------------------------------------------

func (a *App) fetchPlayer() Player {
	now := time.Now()
	switch {
	case a.demo:
		return a.dp.State(now)
	case !a.spot.Available():
		return Player{Volume: -1, Hint: "Spotify is not set up yet", FetchedAt: now}
	}
	return a.spot.State(now)
}

func (a *App) poll() {
	p := a.fetchPlayer()
	a.post(func() { a.applyPlayer(p) })
}

func (a *App) applyPlayer(p Player) {
	if a.pressed != "" {
		return // a command is running; its own result will replace this
	}
	if p.Err != "" && a.player.Active {
		a.setStatus(p.Err) // keep the last good info on screen
		return
	}
	if a.early.from != "" {
		if time.Now().Before(a.early.until) && p.Active && p.Playing && lyricsKey(p) == a.early.from {
			return // Spotify still reports the old song; the next one is already on screen
		}
		a.early = earlySwap{}
	}
	a.setPlayer(p)
}

// setPlayer stores new playback info and reacts to a track change.
func (a *App) setPlayer(p Player) {
	oldKey := a.lyricsKeyNow()
	a.player = p
	a.art.Request(p.ArtURL, a.signal)
	if key := a.lyricsKeyNow(); key != oldKey {
		a.lyrAuto, a.lyrPage = true, 0
		if a.view == ViewLyrics {
			a.ensureLyrics()
		}
	}
}

func (a *App) runAction(act string) {
	var p Player
	var err error
	switch {
	case a.demo:
		a.dp.Act(act, time.Now())
		p = a.dp.State(time.Now())
	case !a.spot.Available():
		p = Player{Volume: -1, Hint: "Spotify is not set up yet"}
		err = os.ErrNotExist
	default:
		p, err = a.spot.Act(act, time.Now())
	}
	a.post(func() {
		a.pressed = ""
		a.setPlayer(p)
		if err != nil {
			a.setStatus(err.Error())
		}
	})
}

// setStatus shows a short message over the bottom of the screen for a few seconds.
func (a *App) setStatus(msg string) {
	a.status, a.statusAt = msg, time.Now()
	at := a.statusAt
	time.AfterFunc(7*time.Second, func() {
		a.post(func() {
			if a.statusAt.Equal(at) {
				a.status = ""
			}
		})
	})
}

// --- library ----------------------------------------------------------------------------

func (a *App) libraryAction(act string) {
	num := func(prefix string) int { n, _ := strconv.Atoi(strings.TrimPrefix(act, prefix)); return n }
	switch {
	case strings.HasPrefix(act, "lib_sec:"):
		if n := num("lib_sec:"); n >= 0 && n < libSections {
			a.libSec, a.libPage = LibSection(n), 0
			a.saveState()
			a.ensureLibrary(a.libSec, false)
		}
	case act == "lib_prev":
		a.libPage = max(0, a.libPage-1)
	case act == "lib_next":
		a.libPage = min(max(0, a.libPages-1), a.libPage+1)
	case act == "lib_reload":
		a.ensureLibrary(a.libSec, true)
	case strings.HasPrefix(act, "lib:"):
		a.playLibItem(num("lib:"))
		return
	}
	a.draw(false)
}

func (a *App) ensureLibrary(sec LibSection, force bool) {
	l := &a.lib[sec]
	ttl := 5 * time.Minute
	if sec == LibDevices {
		ttl = 15 * time.Second
	}
	if l.Loading || (!force && l.Loaded && time.Since(l.At) < ttl) {
		return
	}
	l.Loading, l.Err = true, ""
	go func() {
		items, err := a.fetchLibrary(sec)
		a.post(func() {
			l := &a.lib[sec]
			l.Loading, l.Loaded, l.At = false, true, time.Now()
			if err != nil {
				l.Err = err.Error() // keep the old rows, if any
			} else {
				l.Items, l.Err = items, ""
			}
		})
	}()
}

func (a *App) fetchLibrary(sec LibSection) ([]LibItem, error) {
	switch {
	case a.demo:
		return a.dp.Library(sec), nil
	case !a.spot.Available():
		return nil, fmt.Errorf("Spotify is not set up yet")
	}
	switch sec {
	case LibPlaylists:
		return a.spot.Playlists()
	case LibAlbums:
		return a.spot.Albums()
	case LibLiked:
		return a.spot.Liked()
	case LibRecent:
		return a.spot.Recent()
	}
	return a.spot.Devices()
}

// playLibItem starts what was tapped: a playlist or album, a track (and those after it), or a device.
func (a *App) playLibItem(idx int) {
	items := a.lib[a.libSec].Items
	if idx < 0 || idx >= len(items) || a.pressed != "" {
		return
	}
	it := items[idx]
	var run func() error
	toMusic := true
	switch it.Kind {
	case KindDevice:
		toMusic = false
		run = func() error {
			if a.demo {
				a.dp.Transfer(it.DeviceID)
				return nil
			}
			return a.spot.Transfer(it.DeviceID, a.player.Playing)
		}
	case KindContext:
		run = func() error {
			if a.demo {
				a.dp.PlayItem(it)
				return nil
			}
			return a.spot.PlayContext(it.URI)
		}
	default:
		var uris []string
		for _, t := range items[idx:] {
			if t.Kind == KindTrack {
				uris = append(uris, t.URI)
			}
		}
		run = func() error {
			if a.demo {
				a.dp.PlayItem(it)
				return nil
			}
			return a.spot.PlayTracks(uris)
		}
	}
	a.pressed = "play"
	if it.Kind == KindDevice {
		a.setStatus("Switching to " + it.Title + "…")
	} else {
		a.setStatus("Starting " + it.Title + "…")
	}
	a.draw(false)
	go func() {
		err := run()
		time.Sleep(900 * time.Millisecond) // Spotify takes a moment to report the new state
		p := a.fetchPlayer()
		a.post(func() {
			a.pressed = ""
			a.status = ""
			if err != nil {
				a.setStatus(err.Error())
			}
			if p.Err == "" || !a.player.Active {
				a.setPlayer(p)
			}
			a.lib[LibDevices].Loaded = false
			if err == nil && toMusic {
				a.setView(ViewMusic)
			} else if it.Kind == KindDevice {
				a.ensureLibrary(LibDevices, true)
			}
		})
	}()
}

// --- lyrics ------------------------------------------------------------------------------

func (a *App) lyricsKeyNow() string {
	if !a.player.Active || a.player.Title == "" {
		return ""
	}
	return lyricsKey(a.player)
}

// ensureLyrics looks the current track's lyrics up once and remembers them.
func (a *App) ensureLyrics() {
	if a.player.Active {
		a.ensureLyricsFor(a.player)
	}
}

// ensureLyricsFor does the same for any song, so the next one can be ready before it starts.
func (a *App) ensureLyricsFor(p Player) {
	if !a.cfg.UseLyrics() || p.Title == "" {
		return
	}
	key := lyricsKey(p)
	if _, ok := a.lyrCache[key]; ok || a.lyrBusy[key] {
		return
	}
	a.lyrBusy[key] = true
	delete(a.lyrErr, key)
	go func() {
		var l *Lyrics
		var err error
		if a.demo {
			l = demoLyrics()
		} else {
			l, err = FetchLyrics(p.Title, p.Artist, p.Album, p.Dur)
		}
		a.post(func() {
			delete(a.lyrBusy, key)
			if err != nil {
				a.lyrErr[key] = err.Error()
				return
			}
			a.lyrCache[key] = l
			a.logf("lyrics for %q: found=%v synced=%v lines=%d", p.Title, l.Found, l.Synced, len(l.Lines))
		})
	}()
}

func (a *App) lyricsAction(act string) {
	switch act {
	case "lyr_prev":
		a.lyrAuto, a.lyrPage = false, max(0, a.lyrShown-1)
	case "lyr_next":
		a.lyrAuto, a.lyrPage = false, min(max(0, a.lyrPages-1), a.lyrShown+1)
	case "lyr_auto":
		a.lyrAuto = true
	case "lyr_size":
		for i, sz := range lyricSizes {
			if sz == a.lyrSize {
				a.lyrSize = lyricSizes[(i+1)%len(lyricSizes)]
				break
			}
		}
		if !a.lyrAuto {
			a.lyrPage = 0
		}
		a.saveState()
	case "lyr_hl":
		a.lyrHL = !a.lyrHL
		a.saveState()
	}
	a.draw(false)
}

// --- calendar browsing ---------------------------------------------------------------------

// calendarAction handles taps on the Calendar screen: months, days, events and pages.
func (a *App) calendarAction(act string) {
	v := &a.calv
	loc := a.cfg.Loc
	now := time.Now().In(loc)
	back, ahead := a.cfg.CalendarBrowseBack, a.cfg.CalendarBrowseAhead
	switch {
	case act == "cal_prev":
		v.Off, v.Day, v.Page = max(v.Off-1, -back), time.Time{}, 0
	case act == "cal_next":
		v.Off, v.Day, v.Page = min(v.Off+1, ahead), time.Time{}, 0
	case act == "cal_today":
		*v = CalView{}
	case strings.HasPrefix(act, "cal_day:"):
		if d, err := time.ParseInLocation("20060102", strings.TrimPrefix(act, "cal_day:"), loc); err == nil {
			v.Day, v.Page = d, 0
		}
	case act == "cal_dprev" || act == "cal_dnext":
		step := 1
		if act == "cal_dprev" {
			step = -1
		}
		d := v.Day.AddDate(0, 0, step)
		if m := monthsBetween(now, d); !v.Day.IsZero() && m >= -back && m <= ahead {
			v.Day, v.Off, v.Page = d, m, 0
		}
	case strings.HasPrefix(act, "cal_ev:"):
		if i, err := strconv.Atoi(strings.TrimPrefix(act, "cal_ev:")); err == nil && i >= 0 && i < len(a.calRows) {
			row := a.calRows[i]
			v.Detail, v.DPage = &row, 0
		}
	case act == "cal_back":
		v.Detail = nil
	case act == "cal_page_prev":
		v.Page = max(0, v.Page-1)
	case act == "cal_page_next":
		v.Page = min(v.Page+1, max(0, a.calPages-1))
	case act == "cal_dpage_prev":
		v.DPage = max(0, v.DPage-1)
	case act == "cal_dpage_next":
		v.DPage = min(v.DPage+1, max(0, a.calDPages-1))
	}
	a.draw(false)
}

// --- timers -------------------------------------------------------------------------------

func (a *App) background() {
	var polling, calBusy atomic.Bool
	var lastPoll, lastBatt time.Time
	lastPoll = time.Now()
	for range time.Tick(time.Second) {
		now := time.Now()
		if int32(now.In(a.cfg.Loc).Minute()) != a.minuteSeen.Load() {
			a.signal() // the clock moved on
		}
		if a.cal.Stale(now) && calBusy.CompareAndSwap(false, true) {
			go func() {
				a.cal.Refresh(time.Now())
				calBusy.Store(false)
				a.signal()
			}()
		}
		every := time.Duration(a.cfg.PollSeconds) * time.Second
		view := View(a.viewNow.Load())
		switch {
		case view == ViewCalendar || view == ViewSettings || view == ViewLibrary:
			every = 90 * time.Second // the music is not on screen: look rarely
		case !a.playingNow.Load():
			every = 4 * every
		}
		if now.Sub(lastPoll) >= every && polling.CompareAndSwap(false, true) {
			lastPoll = now
			go func() {
				a.poll()
				polling.Store(false)
			}()
		}
		if now.Sub(lastBatt) >= time.Minute {
			lastBatt = now
			if b := readBattery(); b != a.battery {
				v := b
				a.post(func() { a.battery = v })
			}
		}
	}
}

func readBattery() int {
	if out, err := exec.Command("lipc-get-prop", "com.lab126.powerd", "battLevel").Output(); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
			return n
		}
	}
	if matches, _ := filepath.Glob("/sys/class/power_supply/*/capacity"); len(matches) > 0 {
		for _, m := range matches {
			if raw, err := os.ReadFile(m); err == nil {
				if n, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
					return n
				}
			}
		}
	}
	return -1
}

// --- drawing -------------------------------------------------------------------------------

func (a *App) scene(now time.Time) *Scene {
	s := &Scene{Now: now, Cfg: a.cfg, View: a.view, Orient: a.orient, Agenda: a.cal.Agenda(now),
		Player: a.player, Battery: a.battery, Status: a.status, Pressed: a.pressed,
		Light: a.lightSt, Night: a.night, Out: &SceneOut{}}
	if a.seekKey != "" && a.seekKey == a.lyricsKeyNow() {
		s.SeekAnchor = a.seekAnchor
	}
	url := a.player.ArtURL
	s.ArtAt = func(size int) *image.Gray { return a.art.Get(url, size) }
	if a.demo && a.battery < 0 {
		s.Battery = 78
	}
	s.Library = LibView{Section: a.libSec, Page: a.libPage, List: a.lib[a.libSec]}
	key := a.lyricsKeyNow()
	s.Lyrics = LyricsView{L: a.lyrCache[key], Loading: a.lyrBusy[key], Err: a.lyrErr[key], Disabled: !a.cfg.UseLyrics(),
		Page: a.lyrPage, Auto: a.lyrAuto, Size: a.lyrSize, Highlight: a.lyrHL}
	s.Cal = a.calv
	return s
}

func (a *App) draw(force bool) {
	now := time.Now().In(a.cfg.Loc)
	a.minuteSeen.Store(int32(now.Minute()))
	a.viewNow.Store(int32(a.view))
	a.playingNow.Store(a.player.Playing)

	s := a.scene(now)
	canvas, hits := Render(s)
	switch a.view {
	case ViewLibrary:
		a.libRows, a.libPage, a.libPages = s.Out.LibRows, s.Out.LibPage, s.Out.LibPages
	case ViewLyrics:
		a.lyrShown, a.lyrPages, a.lyrTurns = s.Out.LyrPage, s.Out.LyrPages, s.Out.LyrTurns
	case ViewCalendar:
		a.calRows, a.calPages, a.calDPages = s.Out.CalRows, s.Out.CalPages, s.Out.CalDPages
		if a.calv.Detail != nil {
			a.calv.DPage = s.Out.CalDPage
		} else {
			a.calv.Page = s.Out.CalPage
		}
	}
	a.mu.Lock()
	a.last, a.lastOrient, a.hits = canvas.Img, a.orient, hits
	a.version++
	a.mu.Unlock()

	dev := ToDevice(canvas.Img, a.orient)
	full := force || a.lastDev == nil || a.lastDev.Bounds() != dev.Bounds() || a.partials >= a.cfg.FullRefreshEvery
	if !full {
		rects, big := diffRects(a.lastDev, dev)
		if big {
			full = true
		} else {
			for _, r := range rects {
				t0 := time.Now()
				if err := a.disp.Partial(dev, r); err != nil {
					a.logf("partial update: %v", err)
				}
				if d := time.Since(t0); d > 700*time.Millisecond {
					a.logf("slow partial update %v (%dx%d)", d.Round(time.Millisecond), r.Dx(), r.Dy())
				}
				a.partials++
			}
		}
	}
	if full {
		t0 := time.Now()
		if err := a.disp.Full(dev); err != nil {
			a.logf("full update: %v", err)
		}
		a.logf("full refresh: %v (view %d, %s)", time.Since(t0).Round(time.Millisecond), a.view, a.orient)
		a.partials = 0
		// keep what was drawn, upright, so problems can be looked at later
		_ = writePNG(filepath.Join(a.cfg.Dir, "last-view.png"), canvas.Img)
	}
	a.lastDev = dev
	a.scheduleLyricsTick()
	a.prepareNext()
	a.armEndPoll()
}

// lyricsDelay says how long until the lyrics need redrawing: the next moment something on the screen
// changes (the highlight moving to the next line, if it is switched on, or a page turn while following),
// less the settle delay, so it is already on screen when it is sung.
func (a *App) lyricsDelay(now time.Time) (time.Duration, bool) {
	if a.view != ViewLyrics || !a.player.Playing || !a.cfg.UseLyrics() {
		return 0, false
	}
	l := a.lyrCache[a.lyricsKeyNow()]
	if l == nil || !l.Synced {
		return 0, false
	}
	pos := a.player.Expected(now)
	var due time.Duration
	have := false
	consider := func(at time.Duration) {
		if at > pos && (!have || at < due) {
			due, have = at, true
		}
	}
	if a.lyrHL {
		lead := lyricsLead(a.cfg)
		if next := l.CurrentIndex(pos+lead) + 1; next < len(l.Lines) {
			consider(l.Lines[next].At - lead)
		}
	}
	if a.lyrAuto {
		for _, at := range a.lyrTurns {
			consider(at)
		}
	}
	if !have {
		return 0, false
	}
	d := due - pos - settleDelay + 80*time.Millisecond
	return min(max(d, 150*time.Millisecond), 20*time.Second), true // re-check now and then in case playback moved
}

func (a *App) scheduleLyricsTick() {
	if a.lyrTimer != nil {
		a.lyrTimer.Stop()
		a.lyrTimer = nil
	}
	if d, ok := a.lyricsDelay(time.Now()); ok {
		a.lyrTimer = time.AfterFunc(d, a.signal)
	}
}

// --- showing the next song early ---------------------------------------------------------------------

// nextLookahead is how long before a song ends that the next one is looked up.
const nextLookahead = 25 * time.Second

// earlySwap remembers that the next song was put on screen before Spotify said so.
type earlySwap struct {
	from  string    // the song Spotify last said was playing
	until time.Time // after this, believe Spotify again whatever it says
}

func (a *App) musicShown() bool {
	return a.view == ViewHome || a.view == ViewMusic || a.view == ViewLyrics
}

func (a *App) nextLead() time.Duration {
	return time.Duration(a.cfg.NextTrackLead * float64(time.Second))
}

func (a *App) fetchNextTrack() (Player, bool, error) {
	now := time.Now()
	switch {
	case a.demo:
		return a.dp.NextTrack(now), true, nil
	case !a.spot.Available():
		return Player{}, false, nil
	}
	return a.spot.NextTrack(now)
}

// prepareNext runs after every draw. Near the end of a playing song it finds out what comes next, warms that
// cover (and its lyrics, if the lyrics screen is up) and arms a timer that puts it on screen a few seconds
// early, because the e-ink panel needs a moment to change.
func (a *App) prepareNext() {
	if a.nextTimer != nil {
		a.nextTimer.Stop()
		a.nextTimer = nil
	}
	p := a.player
	lead := a.nextLead()
	if lead < 0 || !a.musicShown() || !p.Active || !p.Playing || p.Dur <= 0 {
		return
	}
	key := a.lyricsKeyNow()
	left := p.Dur - p.Expected(time.Now())
	if left > nextLookahead || left < 400*time.Millisecond {
		return
	}
	if a.nextFor != key {
		if !a.nextBusy {
			a.lookUpNext(key)
		}
		return
	}
	if a.nextTrack.Title == "" {
		return // nothing queued
	}
	a.nextTimer = time.AfterFunc(max(0, left-lead), func() { a.post(func() { a.showNextEarly(key) }) })
}

func (a *App) lookUpNext(key string) {
	a.nextBusy = true
	go func() {
		np, ok, err := a.fetchNextTrack()
		a.post(func() {
			a.nextBusy = false
			a.nextFor, a.nextTrack = key, Player{} // one try per song, whatever happens
			if err != nil {
				a.logf("next song lookup: %v", err)
				return
			}
			if !ok {
				// Spotify hides what is next for playlists you do not own (and some it makes itself); the
				// app then relies on asking right when the song ends (armEndPoll).
				a.logf("next song: Spotify does not list one")
				return
			}
			if lyricsKey(np) == key {
				return // the same song again
			}
			a.nextTrack = np
			a.art.Request(np.ArtURL, nil)
			if a.view == ViewLyrics {
				a.ensureLyricsFor(np)
			}
			a.logf("next song: %q", np.Title)
		})
	}()
}

const (
	endPollDelay = 400 * time.Millisecond  // after the song should have ended
	endPollEvery = 1200 * time.Millisecond // then again, until it has changed
	endPollTries = 8
)

// armEndPoll is for when the next song is not known in advance. Instead of waiting for the next regular
// poll (up to ten seconds), it asks Spotify just after the song should end, and keeps asking every second
// or so until the new song shows up.
func (a *App) armEndPoll() {
	if a.endTimer != nil {
		a.endTimer.Stop()
		a.endTimer = nil
	}
	p := a.player
	if !a.musicShown() || !p.Active || !p.Playing || p.Dur <= 0 {
		return
	}
	key := a.lyricsKeyNow()
	left := p.Dur - p.Expected(time.Now())
	a.endTimer = time.AfterFunc(left+endPollDelay, func() { a.pollUntilChanged(key) })
}

// pollUntilChanged asks Spotify what is playing until it is not the song with oldKey any more (or the
// music stops, or the screen changes), applying each answer as it arrives.
func (a *App) pollUntilChanged(oldKey string) {
	if !a.endBusy.CompareAndSwap(false, true) {
		return
	}
	defer a.endBusy.Store(false)
	for i := 1; i <= endPollTries; i++ {
		p := a.fetchPlayer()
		done := make(chan bool, 1)
		a.post(func() {
			a.applyPlayer(p)
			changed := a.lyricsKeyNow() != oldKey
			if changed {
				a.logf("song change noticed on check %d after the expected end", i)
			}
			done <- changed || !a.player.Playing || !a.musicShown()
		})
		if <-done {
			return
		}
		time.Sleep(endPollEvery)
	}
}

// showNextEarly puts the next song on screen a little before it starts. Spotify's own answer takes over
// as soon as it reports the new song, or after a few seconds if it does not.
func (a *App) showNextEarly(key string) {
	if a.lyricsKeyNow() != key || !a.player.Playing || a.pressed != "" || !a.musicShown() ||
		a.nextFor != key || a.nextTrack.Title == "" {
		return
	}
	now := time.Now()
	left := max(0, a.player.Dur-a.player.Expected(now)) // time until the current song ends
	np := a.nextTrack
	np.Active, np.Playing = true, true
	np.Device, np.Volume = a.player.Device, a.player.Volume
	np.Progress, np.FetchedAt = -left, now // counts up from before the song begins: zero when it starts
	a.logf("showing the next song %v early", left.Round(100*time.Millisecond))
	a.setPlayer(np)
	a.early = earlySwap{from: key, until: now.Add(left + 10*time.Second)}
	time.AfterFunc(left+1500*time.Millisecond, func() { a.poll() }) // soon after it starts, check what Spotify really plays
}

// Snapshot gives the simulator the latest logical frame.
func (a *App) Snapshot() (img *image.Gray, o Orient, version int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last, a.lastOrient, a.version
}

// --- remembering the last view and choices ----------------------------------------------------

type savedState struct {
	View    int   `json:"view"`
	Orient  int   `json:"orient"`
	Night   bool  `json:"night"`
	Clock24 *bool `json:"clock24,omitempty"`
	LibSec  int   `json:"lib_section"`
	LyrSize int   `json:"lyrics_size"`
	LyrHL   bool  `json:"lyrics_line"`
}

func (a *App) loadState() {
	raw, err := os.ReadFile(a.cfg.StateFile)
	if err != nil {
		return
	}
	var s savedState
	if json.Unmarshal(raw, &s) != nil {
		return
	}
	if s.View >= 0 && s.View <= int(ViewLyrics) && s.Orient >= 0 && s.Orient <= 2 {
		a.view, a.orient, a.night = View(s.View), Orient(s.Orient), s.Night
	}
	if s.Clock24 != nil {
		a.cfg.Clock24 = s.Clock24
	}
	if s.LibSec >= 0 && s.LibSec < libSections {
		a.libSec = LibSection(s.LibSec)
	}
	for _, sz := range lyricSizes {
		if sz == s.LyrSize {
			a.lyrSize = sz
		}
	}
	a.lyrHL = s.LyrHL
}

func (a *App) saveState() {
	use24 := a.cfg.Use24h()
	raw, _ := json.Marshal(savedState{View: int(a.view), Orient: int(a.orient), Night: a.night, Clock24: &use24,
		LibSec: int(a.libSec), LyrSize: a.lyrSize, LyrHL: a.lyrHL})
	if err := os.WriteFile(a.cfg.StateFile, raw, 0o644); err != nil {
		a.logf("save state: %v", err)
	}
}
