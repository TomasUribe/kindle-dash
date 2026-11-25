package main

import (
	"fmt"
	"image"
	"strings"
	"time"
)

type View int

const (
	ViewHome View = iota
	ViewCalendar
	ViewMusic
	ViewSettings
	ViewLibrary
	ViewLyrics
)

// viewOrder is the order of the tabs, and of swipes left and right.
var viewOrder = []View{ViewHome, ViewCalendar, ViewMusic, ViewLibrary, ViewLyrics, ViewSettings}

// stepView moves delta places along viewOrder, wrapping around.
func stepView(v View, delta int) View {
	for i, w := range viewOrder {
		if w == v {
			return viewOrder[(i+delta+len(viewOrder))%len(viewOrder)]
		}
	}
	return ViewHome
}

func (v View) Action() string {
	return [...]string{"view:home", "view:calendar", "view:music", "view:settings", "view:library", "view:lyrics"}[v]
}

type Orient int

const (
	Portrait     Orient = iota
	LandscapeCW         // top of the picture turned to the right
	LandscapeCCW        // top of the picture turned to the left
)

func (o Orient) Landscape() bool { return o != Portrait }

// Screen is the Paperwhite 5 panel in portrait.
const (
	ScreenW = 1236
	ScreenH = 1648
)

// HitZone is a tappable rectangle in canvas coordinates.
type HitZone struct {
	R      Rect
	Action string
}

// Scene is everything one frame needs.
type Scene struct {
	Now     time.Time
	Cfg     *Config
	View    View
	Orient  Orient
	Agenda  Agenda
	Player  Player
	ArtAt   func(size int) *image.Gray // album cover at a square size, nil if not loaded
	Battery int                        // percent, -1 unknown
	Status  string
	Pressed string // action of the button being pressed
	Light   LightState
	Night   bool // inverted display
	Library LibView
	Lyrics  LyricsView
	Cal     CalView
	Out     *SceneOut // filled in by Render: paging facts the app needs for its buttons

	// SeekAnchor is where the last seek landed on this track. The progress line steps from there, not
	// from the start of the song, so it is drawn exactly where the finger was.
	SeekAnchor time.Duration

	artRects []Rect // where album art was drawn, so night mode can leave it alone
}

// SceneOut carries facts only the renderer knows (how many rows fit, which page it showed).
type SceneOut struct {
	LibRows, LibPage, LibPages int
	LyrPage, LyrPages, LyrLine int             // the page shown and the line marked as being sung (-1: none)
	LyrTurns                   []time.Duration // song time at which pages 1.. come up while following
	CalRows                    []Row           // events listed on the Calendar screen, in tap order
	CalPage, CalPages          int             // page of the event list shown, and how many there are
	CalDPage, CalDPages        int             // the same for the text of an open event
}

func (s *Scene) out() *SceneOut {
	if s.Out == nil {
		s.Out = &SceneOut{}
	}
	return s.Out
}

// shownProgress is the progress as drawn. It moves in coarse steps because e-ink redraws are slow.
func (s *Scene) shownProgress() time.Duration {
	d := s.Player.Expected(s.Now)
	if step := time.Duration(s.Cfg.ProgressSeconds) * time.Second; step > 0 {
		base := s.SeekAnchor
		if d+3*time.Second >= base { // a player may land a moment before the requested spot
			d = base + max(0, d-base)/step*step
		} else {
			d = d / step * step
		}
	}
	return d
}

func (s *Scene) shownFrac() float64 {
	if !s.Player.Active || s.Player.Dur <= 0 {
		return 0
	}
	return float64(s.shownProgress()) / float64(s.Player.Dur)
}

type layout struct {
	W, H         int
	Land         bool
	M            int
	Header, Tabs Rect
	Content      Rect
	Status       Rect
}

func newLayout(s *Scene) layout {
	l := layout{W: ScreenW, H: ScreenH, M: 52}
	if s.Orient.Landscape() {
		l.W, l.H, l.Land, l.M = ScreenH, ScreenW, true, 48
	}
	headH, tabH := 128, 112
	if l.Land {
		headH, tabH = 100, 100
	}
	l.Header = Rect{l.M, 30, l.W - 2*l.M, headH}
	l.Tabs = Rect{l.M, l.H - 26 - tabH, l.W - 2*l.M, tabH}
	top := l.Header.Bottom() + 18
	bottom := l.Tabs.Y - 24
	l.Content = Rect{l.M, top, l.W - 2*l.M, bottom - top}
	// messages float over the bottom of the content, so showing one never moves anything
	l.Status = Rect{l.M + 20, bottom - 70, l.W - 2*l.M - 40, 60}
	return l
}

// Render draws one frame and returns the tappable zones.
func Render(s *Scene) (*Canvas, []HitZone) {
	l := newLayout(s)
	c := NewCanvas(l.W, l.H)
	var hits []HitZone
	drawHeader(c, l, s)
	hits = append(hits, HitZone{l.Header, "refresh"})
	switch s.View {
	case ViewHome:
		drawHome(c, l, s, &hits)
	case ViewCalendar:
		drawCalendar(c, l, s, &hits)
	case ViewMusic:
		drawMusic(c, l, s, &hits)
	case ViewSettings:
		drawSettings(c, l, s, &hits)
	case ViewLibrary:
		drawLibrary(c, l, s, &hits)
	case ViewLyrics:
		drawLyrics(c, l, s, &hits)
	}
	if s.Status != "" {
		f := Face(Medium, 28)
		c.Card(l.Status, 30, 0, Black, Black)
		c.TextIn(s.Status, Rect{l.Status.X + 24, l.Status.CY() - LineHeight(f)/2, l.Status.W - 48, LineHeight(f)}, f, White, Center)
	}
	drawTabs(c, l, s, &hits)
	if s.Night {
		invertCanvas(c, s.artRects)
	}
	return c, hits
}

// invertCanvas swaps black and white, then flips the album art back so it is not a negative.
func invertCanvas(c *Canvas, keep []Rect) {
	flip := func(r image.Rectangle) {
		r = r.Intersect(c.Img.Bounds())
		for y := r.Min.Y; y < r.Max.Y; y++ {
			row := c.Img.Pix[y*c.Img.Stride : y*c.Img.Stride+c.W]
			for x := r.Min.X; x < r.Max.X; x++ {
				row[x] = 255 - row[x]
			}
		}
	}
	flip(c.Img.Bounds())
	for _, r := range keep {
		flip(r.Image())
	}
}

// --- header and tabs ---------------------------------------------------------

func drawHeader(c *Canvas, l layout, s *Scene) {
	now := s.Now.In(s.Cfg.Loc)
	size := 128
	if l.Land {
		size = 100
	}
	tf := Face(Light, size)
	base := l.Header.Y + Ascent(tf) - size/8
	var clock, suffix string
	if s.Cfg.Use24h() {
		clock = now.Format("15:04")
	} else {
		clock = strings.TrimPrefix(now.Format("03:04"), "0")
		suffix = strings.ToLower(now.Format("PM"))
	}
	c.Text(clock, l.Header.X-4, base, tf, Black)
	if suffix != "" {
		c.Text(suffix, l.Header.X+TextWidth(tf, clock)+8, base, Face(Medium, size/3), Dark)
	}

	right := l.Header.Right()
	wf, df, sf := Face(Medium, 40), Face(Regular, 32), Face(Regular, 25)
	if l.Land {
		wf, df, sf = Face(Medium, 36), Face(Regular, 30), Face(Regular, 23)
	}
	y := l.Header.Y + 6
	c.TextIn(now.Format("Monday"), Rect{right - 600, y, 600, LineHeight(wf)}, wf, Black, Right)
	y += LineHeight(wf) - 6
	c.TextIn(now.Format("2 January"), Rect{right - 600, y, 600, LineHeight(df)}, df, Mid, Right)
	y += LineHeight(df) - 2

	x := right
	if s.Battery >= 0 {
		txt := fmt.Sprintf("%d%%", s.Battery)
		x -= TextWidth(sf, txt)
		c.Text(txt, x, y+Ascent(sf)-2, sf, Mid)
		x -= 50
		IconBattery(c, x, y+3, 40, 20, s.Battery, Mid)
		x -= 22
	}
	if s.Agenda.Demo || s.Player.Demo {
		x -= TextWidth(sf, "DEMO")
		c.Text("DEMO", x, y+Ascent(sf)-2, sf, Soft)
	}
}

func drawTabs(c *Canvas, l layout, s *Scene, hits *[]HitZone) {
	type tab struct {
		label, action string
		icon          func(c *Canvas, cx, cy, sz float64, v, bg uint8)
	}
	tabs := []tab{
		{"Home", ViewHome.Action(), func(c *Canvas, cx, cy, sz float64, v, bg uint8) { IconHome(c, cx, cy, sz, v, bg) }},
		{"Calendar", ViewCalendar.Action(), func(c *Canvas, cx, cy, sz float64, v, bg uint8) { IconCalendar(c, cx, cy, sz, v, bg) }},
		{"Music", ViewMusic.Action(), func(c *Canvas, cx, cy, sz float64, v, _ uint8) { IconMusic(c, cx, cy, sz, v) }},
		{"Library", ViewLibrary.Action(), func(c *Canvas, cx, cy, sz float64, v, bg uint8) { IconLibrary(c, cx, cy, sz, v, bg) }},
		{"Lyrics", ViewLyrics.Action(), func(c *Canvas, cx, cy, sz float64, v, bg uint8) { IconLyrics(c, cx, cy, sz, v, bg) }},
		{"Settings", ViewSettings.Action(), func(c *Canvas, cx, cy, sz float64, v, bg uint8) { IconGear(c, cx, cy, sz, v, bg) }},
		{"Rotate", "rotate", func(c *Canvas, cx, cy, sz float64, v, _ uint8) { IconRotate(c, cx, cy, sz, v) }},
		{"Exit", "exit", func(c *Canvas, cx, cy, sz float64, v, _ uint8) { IconPower(c, cx, cy, sz, v) }},
	}
	hairline(c, l.Tabs.X, l.Tabs.Y-10, l.Tabs.W, Pale)
	w := l.Tabs.W / len(tabs)
	lf, lfb := Face(Regular, 21), Face(Medium, 21)
	pillW, pillH := min(w-10, 110), 62
	iconSize := 40.0
	if l.Land {
		pillH, iconSize = 56, 38
	}
	for i, t := range tabs {
		slot := Rect{l.Tabs.X + i*w, l.Tabs.Y, w, l.Tabs.H}
		on := t.action == s.View.Action() || t.action == s.Pressed
		pill := Rect{slot.CX() - pillW/2, slot.Y + 6, pillW, pillH}
		ink, label, face := Black, Mid, lf
		bg := uint8(White)
		if on {
			c.RoundRect(pill, pillH/2, Black)
			ink, bg, label, face = White, Black, Black, lfb
		}
		t.icon(c, float64(pill.CX()), float64(pill.CY()), iconSize, ink, bg)
		c.TextIn(t.label, Rect{slot.X, pill.Bottom() + 6, slot.W, LineHeight(face)}, face, label, Center)
		*hits = append(*hits, HitZone{slot, t.action})
	}
}
