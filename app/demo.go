package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// demoEvents mirrors a believable week, anchored on "now" so current/past styling shows.
func demoEvents(now time.Time, loc *time.Location) []Event {
	now = now.In(loc)
	at := func(dayOffset, hour, minute int) time.Time {
		d := now.AddDate(0, 0, dayOffset)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, loc)
	}
	h := now.Hour()
	clamp := func(v, lo, hi int) int { return max(lo, min(hi, v)) }
	timed := func(cal, title, where string, s, e time.Time) Event {
		return Event{Title: title, Location: where, Cal: cal, Start: s, End: e}
	}
	whole := func(cal, title string, day, days int) Event {
		return Event{Title: title, Cal: cal, Start: at(day, 0, 0), End: at(day+days, 0, 0), AllDay: true}
	}
	return []Event{
		timed("Work", "Stand-up", "Meet", at(0, clamp(h-3, 0, 20), 0), at(0, clamp(h-3, 0, 20), 15)),
		timed("Work", "Design review", "Room 4B", at(0, h, 0), at(0, clamp(h+1, 0, 23), 59)),
		timed("Personal", "Dentist", "12 Example Street", at(0, clamp(h+3, 0, 21), 30), at(0, clamp(h+4, 0, 22), 15)),
		timed("Family", "Dinner with Sam", "Corner Bistro", at(0, 20, 0), at(0, 22, 0)),
		whole("Family", "Alex's birthday", 1, 1),
		timed("Work", "1:1 with Jordan", "", at(1, 10, 0), at(1, 10, 30)),
		timed("Work", "Sprint planning", "Zoom", at(1, 14, 0), at(1, 15, 30)),
		timed("Travel", "Flight AB 123", "Terminal 1", at(2, 7, 15), at(2, 8, 20)),
		whole("Travel", "Conference trip", 2, 3),
		timed("Training", "Long ride", "", at(3, 6, 0), at(3, 9, 30)),
		timed("Personal", "Climbing", "", at(4, 18, 30), at(4, 20, 0)),
	}
}

// demoPlayer is a tiny fake Spotify that answers the controls, for design work without accounts.
type demoPlayer struct {
	mu      sync.Mutex
	dev     int // which demo device is active
	idx     int
	playing bool
	volume  int
	pos     time.Duration
	stamp   time.Time
}

// The demo plays one real album's track list (titles and lengths are plain facts) with a cover drawn
// in code. Its lyrics are made-up placeholder text, never the songs' real words.
const demoAlbum, demoArtist = "The Dark Side of the Moon", "Pink Floyd"

var demoTracks = []struct {
	title, artist, album string
	dur                  time.Duration
}{
	{"Speak to Me", demoArtist, demoAlbum, 68 * time.Second},
	{"Breathe (In the Air)", demoArtist, demoAlbum, 163 * time.Second},
	{"On the Run", demoArtist, demoAlbum, 225 * time.Second},
	{"Time", demoArtist, demoAlbum, 413 * time.Second},
	{"The Great Gig in the Sky", demoArtist, demoAlbum, 284 * time.Second},
	{"Money", demoArtist, demoAlbum, 383 * time.Second},
	{"Us and Them", demoArtist, demoAlbum, 469 * time.Second},
	{"Any Colour You Like", demoArtist, demoAlbum, 206 * time.Second},
	{"Brain Damage", demoArtist, demoAlbum, 226 * time.Second},
	{"Eclipse", demoArtist, demoAlbum, 123 * time.Second},
}

func newDemoPlayer(now time.Time) *demoPlayer {
	return &demoPlayer{idx: 3, playing: true, volume: 55, pos: 71 * time.Second, stamp: now}
}

func (d *demoPlayer) advance(now time.Time) {
	if d.playing {
		d.pos += now.Sub(d.stamp)
	}
	d.stamp = now
	for d.pos >= demoTracks[d.idx].dur {
		d.pos -= demoTracks[d.idx].dur
		d.idx = (d.idx + 1) % len(demoTracks)
	}
}

func (d *demoPlayer) State(now time.Time) Player {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.advance(now)
	t := demoTracks[d.idx]
	return Player{Demo: true, Active: true, Playing: d.playing, Title: t.title, Artist: t.artist,
		Album: t.album, Device: demoDevices[d.dev].name + " (demo)", Progress: d.pos, Dur: t.dur,
		Volume: d.volume, ArtURL: "demo:prism", FetchedAt: now}
}

func (d *demoPlayer) Act(action string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.advance(now)
	switch action {
	case "toggle":
		d.playing = !d.playing
	case "next":
		d.idx, d.pos = (d.idx+1)%len(demoTracks), 0
	case "prev":
		if d.pos > 3*time.Second {
			d.pos = 0
		} else {
			d.idx, d.pos = (d.idx+len(demoTracks)-1)%len(demoTracks), 0
		}
	case "vol_up":
		d.volume = min(100, d.volume+10)
	case "vol_down":
		d.volume = max(0, d.volume-10)
	default:
		if ms, ok := strings.CutPrefix(action, "seek:"); ok {
			if n, err := strconv.Atoi(ms); err == nil {
				d.pos = time.Duration(n) * time.Millisecond
			}
		}
	}
}

var demoDevices = []struct{ id, name, kind string }{
	{"d1", "Living room speaker", "Speaker"},
	{"d2", "Phone", "Smartphone"},
	{"d3", "Web Player (Firefox)", "Computer"},
}

var demoPlaylistNames = []string{"Morning Coffee", "Focus Flow", "Road Trip", "Sunday Slow", "Workout Mix", "Night Drive",
	"Old Favourites", "Study Beats", "Dinner Party", "Rainy Day", "Throwback Hits", "Quiet Evenings", "Weekend Warmup",
	"Deep Work", "Coffee Shop Jazz", "Long Walks", "Cooking Together"}

var demoAlbums = []struct{ title, artist string }{
	{demoAlbum, demoArtist}, {"Slow Tides", "The Paper Lanterns"}, {"Greenhouse Sessions", "Marisol Vega & The Night Shift"},
	{"Mile Markers", "Hollow Pines"}, {"Small Hours", "Ines Calder"},
	{"Paper Boats", "The Low Tide Club"}, {"Afterglow", "Northern Static"}, {"Field Notes", "Wren & Alder"},
	{"Quiet Machines", "Halcyon Drive"}, {"Terra Firma", "Mosaic Parade"}}

var demoExtraTracks = []struct{ title, artist, album string }{
	{"Time", demoArtist, demoAlbum}, {"Money", demoArtist, demoAlbum}, {"Breathe (In the Air)", demoArtist, demoAlbum},
	{"Small Hours", "Ines Calder", "Small Hours"}, {"Paper Boats", "The Low Tide Club", "Paper Boats"},
	{"Afterglow", "Northern Static", "Afterglow"}, {"Field Notes", "Wren & Alder", "Field Notes"},
	{"Quiet Machines", "Halcyon Drive", "Quiet Machines"}, {"Terra Firma", "Mosaic Parade", "Terra Firma"},
	{"Harbour Lights", "Ines Calder", "Small Hours"}, {"Slow Dancer", "Wren & Alder", "Field Notes"}}

// Library is the fake library shown with -demo.
func (d *demoPlayer) Library(sec LibSection) []LibItem {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []LibItem
	switch sec {
	case LibPlaylists:
		for i, n := range demoPlaylistNames {
			out = append(out, LibItem{Title: n, Sub: map[bool]string{true: "You", false: "Spotify"}[i%3 != 0], URI: fmt.Sprintf("demo:playlist:%d", i), Kind: KindContext})
		}
	case LibAlbums:
		for i, a := range demoAlbums {
			out = append(out, LibItem{Title: a.title, Sub: a.artist, URI: fmt.Sprintf("demo:album:%d", i), Kind: KindContext})
		}
	case LibLiked, LibRecent:
		for _, t := range demoTracks {
			out = append(out, LibItem{Title: t.title, Sub: t.artist + "  \u00b7  " + t.album, URI: "demo:track:" + t.title, Kind: KindTrack})
		}
		for _, t := range demoExtraTracks {
			out = append(out, LibItem{Title: t.title, Sub: t.artist + "  \u00b7  " + t.album, URI: "demo:track:" + t.title, Kind: KindTrack})
		}
		if sec == LibRecent {
			out = out[:8]
		}
	case LibDevices:
		for i, dv := range demoDevices {
			out = append(out, LibItem{Title: dv.name + " (demo)", Sub: dv.kind, Kind: KindDevice, DeviceID: dv.id, Active: i == d.dev})
		}
	}
	return out
}

// PlayItem pretends to start whatever was tapped in the demo library.
func (d *demoPlayer) PlayItem(it LibItem) {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := 0
	for _, r := range it.Title {
		h = h*31 + int(r)
	}
	d.idx, d.pos, d.playing, d.stamp = h%len(demoTracks), 0, true, time.Now()
}

// Transfer pretends to move playback to another demo device.
func (d *demoPlayer) Transfer(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, dv := range demoDevices {
		if dv.id == id {
			d.dev = i
		}
	}
}
