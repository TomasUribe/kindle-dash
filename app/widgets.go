package main

import (
	"fmt"
	"strings"
	"time"
)

// The look: lots of white, thin lines, large type, and black used sparingly (the play button,
// whatever is selected, the line being sung). Plain icons, circles and pills instead of boxes.

type iconFn func(c *Canvas, cx, cy, s float64, v uint8)

func hairline(c *Canvas, x, y, w int, v uint8) { c.Fill(Rect{x, y, w, 3}, v) }

// caption is a small uppercase label.
func caption(c *Canvas, text string, box Rect, v uint8, a Align) {
	c.TextIn(strings.ToUpper(text), box, Face(Medium, 22), v, a)
}

type btnStyle int

const (
	btnPlain    btnStyle = iota // just the icon
	btnOutlined                 // icon in a ring
	btnFilled                   // white icon on a black disc: the main action
)

// roundButton is a circular touch target. While it is being pressed it inverts.
func roundButton(c *Canvas, cx, cy, r int, action string, icon iconFn, iconSize float64, style btnStyle, enabled bool, s *Scene, hits *[]HitZone) {
	pressed := action != "" && action == s.Pressed
	ink, edge := Black, Black
	if !enabled {
		ink, edge = Soft, Soft
	}
	fx, fy, fr := float64(cx), float64(cy), float64(r)
	switch style {
	case btnFilled:
		if pressed {
			c.Circle(fx, fy, fr, Black)
			c.Circle(fx, fy, fr-5, White)
			ink = Black
		} else {
			c.Circle(fx, fy, fr, edge)
			ink = White
		}
	case btnOutlined:
		if pressed {
			c.Circle(fx, fy, fr, Black)
			ink = White
		} else {
			c.Circle(fx, fy, fr, edge)
			c.Circle(fx, fy, fr-3, White)
		}
	default:
		if pressed {
			c.Circle(fx, fy, fr, Black)
			ink = White
		}
	}
	icon(c, fx, fy, iconSize, ink)
	if enabled && action != "" {
		hr := max(r, 60)
		*hits = append(*hits, HitZone{Rect{cx - hr, cy - hr, 2 * hr, 2 * hr}, action})
	}
}

// pillButton is a rounded text button.
func pillButton(c *Canvas, r Rect, label, action string, filled, enabled bool, hits *[]HitZone) {
	fill, ink, edge := White, Black, Black
	if filled {
		fill, ink = Black, White
	}
	if !enabled {
		ink, edge = Soft, Soft
		if filled {
			fill = Soft
			ink = White
		}
	}
	c.Card(r, r.H/2, 3, fill, edge)
	f := Face(Medium, 28)
	c.TextIn(label, Rect{r.X, r.CY() - LineHeight(f)/2, r.W, LineHeight(f)}, f, ink, Center)
	if enabled && action != "" {
		*hits = append(*hits, HitZone{r, action})
	}
}

// pillIconButton is a rounded button with an icon.
func pillIconButton(c *Canvas, r Rect, icon iconFn, iconSize float64, action string, enabled bool, hits *[]HitZone) {
	ink, edge := Black, Black
	if !enabled {
		ink, edge = Soft, Soft
	}
	c.Card(r, r.H/2, 3, White, edge)
	icon(c, float64(r.CX()), float64(r.CY()), iconSize, ink)
	if enabled && action != "" {
		*hits = append(*hits, HitZone{r, action})
	}
}

// segmented is one pill split into choices; the chosen one is filled.
func segmented(c *Canvas, r Rect, labels []string, sel int, enabled bool, actionOf func(i int) string, hits *[]HitZone) {
	c.Card(r, r.H/2, 3, White, Black)
	n := len(labels)
	w := r.W / n
	f := Face(Medium, 28)
	for i, lb := range labels {
		seg := Rect{r.X + i*w, r.Y, w, r.H}
		if i == n-1 {
			seg.W = r.Right() - seg.X
		}
		ink := Black
		if i == sel {
			c.RoundRect(seg.Inset(5), (seg.H-10)/2, Black)
			ink = White
		} else if i > 0 && i-1 != sel {
			c.Fill(Rect{seg.X, r.Y + 18, 3, r.H - 36}, Soft)
		}
		if !enabled && i != sel {
			ink = Soft
		}
		c.TextIn(lb, Rect{seg.X, seg.CY() - LineHeight(f)/2, seg.W, LineHeight(f)}, f, ink, Center)
		if enabled {
			*hits = append(*hits, HitZone{seg, actionOf(i)})
		}
	}
}

// toggleSwitch is an on/off switch.
func toggleSwitch(c *Canvas, r Rect, on bool, action string, hits *[]HitZone) {
	if on {
		c.RoundRect(r, r.H/2, Black)
		c.Circle(float64(r.Right()-r.H/2), float64(r.CY()), float64(r.H/2-9), White)
	} else {
		c.Card(r, r.H/2, 3, White, Black)
		c.Circle(float64(r.X+r.H/2), float64(r.CY()), float64(r.H/2-13), Black)
	}
	*hits = append(*hits, HitZone{Rect{r.X - 20, r.Y - 20, r.W + 40, r.H + 40}, action})
}

// slider is a thin track with a ring knob; tapping anywhere on it jumps to that level.
func slider(c *Canvas, r Rect, level, maxLvl int, key string, enabled bool, hits *[]HitZone) {
	track := Rect{r.X + 34, r.CY() - 6, r.W - 68, 12}
	c.RoundRect(track, 6, Pale)
	frac := 0.0
	if maxLvl > 0 {
		frac = float64(level) / float64(maxLvl)
	}
	kx := track.X + int(float64(track.W)*frac)
	ink := Black
	if !enabled {
		ink = Soft
	}
	if enabled && level > 0 {
		c.RoundRect(Rect{track.X, track.Y, kx - track.X + 6, 12}, 6, Black)
	}
	c.Circle(float64(kx), float64(r.CY()), 30, ink)
	c.Circle(float64(kx), float64(r.CY()), 20, White)
	if enabled && maxLvl > 0 {
		step := float64(track.W) / float64(maxLvl)
		for i := 0; i <= maxLvl; i++ {
			x0 := max(r.X, track.X+int((float64(i)-0.5)*step))
			x1 := min(r.Right(), track.X+int((float64(i)+0.5)*step))
			*hits = append(*hits, HitZone{Rect{x0, r.Y, x1 - x0, r.H}, fmt.Sprintf("%s:%d", key, i)})
		}
	}
}

// thinProgress is a hairline progress bar with a dot at the current point.
func thinProgress(c *Canvas, r Rect, frac float64) {
	frac = max(0, min(1, frac))
	track := Rect{r.X, r.CY() - 4, r.W, 8}
	c.RoundRect(track, 4, Pale)
	fw := int(float64(r.W) * frac)
	if fw > 0 {
		c.RoundRect(Rect{r.X, track.Y, max(fw, 8), 8}, 4, Black)
	}
	kx := max(r.X+13, min(r.Right()-13, r.X+fw))
	c.Circle(float64(kx), float64(r.CY()), 13, Black)
}

// seekAction is the hit-zone action of a progress line. The app turns a tap on it into
// "seek:<per mille of the song>" using where along the line the finger was.
const seekAction = "seek"

// seekBar is the progress line plus a generous touch target: tap (or drag a little) to jump to that point.
func seekBar(c *Canvas, r Rect, s *Scene, hits *[]HitZone) {
	thinProgress(c, r, s.shownFrac())
	if s.Player.Active && s.Player.Dur > 0 {
		*hits = append(*hits, HitZone{Rect{r.X, r.Y - 12, r.W, r.H + 24}, seekAction})
	}
}

// seekPermille turns a horizontal position along a seek zone into a 0..1000 fraction of the song.
func seekPermille(z Rect, x int) int {
	if z.W <= 0 {
		return 0
	}
	return max(0, min(1000, (x-z.X)*1000/z.W))
}

func clockMS(d time.Duration) string {
	sec := int(d.Seconds())
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// progressTimes writes elapsed and total time under a progress line.
func progressTimes(c *Canvas, r Rect, s *Scene) {
	if !s.Player.Active {
		return
	}
	f := Face(Regular, 24)
	c.TextIn(clockMS(s.shownProgress()), Rect{r.X, r.Y, 200, LineHeight(f)}, f, Mid, Left)
	c.TextIn(clockMS(s.Player.Dur), Rect{r.Right() - 200, r.Y, 200, LineHeight(f)}, f, Mid, Right)
}

// artBox draws the album cover with rounded corners, or a quiet placeholder.
func artBox(c *Canvas, r Rect, s *Scene) {
	if s.ArtAt != nil && s.Player.Active {
		if img := s.ArtAt(r.W); img != nil {
			c.RoundRect(r.Inset(-2), 30, Soft) // a hairline edge so pale covers do not melt into the page
			c.DrawImage(img, r, 28)
			s.artRects = append(s.artRects, r.Inset(-2))
			return
		}
	}
	c.RoundRect(r, 28, Pale)
	IconMusic(c, float64(r.CX()), float64(r.CY()), float64(r.W)*0.36, Soft)
}

type playerTexts struct{ title, artist, album, device string }

func playerTexts_(s *Scene) playerTexts {
	p := s.Player
	if !p.Active {
		sub := p.Hint
		if p.Err != "" {
			sub = p.Err
		}
		return playerTexts{title: "Nothing playing", artist: sub}
	}
	return playerTexts{title: orDash(p.Title), artist: p.Artist, album: p.Album, device: p.Device}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// stateCaption says what the player is doing, in small capitals.
func stateCaption(p Player) string {
	switch {
	case !p.Active:
		return ""
	case p.Playing:
		return "Now playing"
	}
	return "Paused"
}

// heroText stacks title (up to two lines), artist and album. It returns the y just below the last line.
func heroText(c *Canvas, r Rect, t playerTexts, a Align, titleSize, artistSize, smallSize int, centerV bool) int {
	tf, af, sf := Face(Bold, titleSize), Face(Medium, artistSize), Face(Regular, smallSize)
	lines := Wrap(tf, t.title, r.W, 2)
	total := len(lines)*(LineHeight(tf)-4) + 8
	if t.artist != "" {
		total += LineHeight(af)
	}
	if t.album != "" {
		total += LineHeight(sf)
	}
	y := r.Y
	if centerV && total < r.H {
		y += (r.H - total) / 2
	}
	for _, ln := range lines {
		c.TextIn(ln, Rect{r.X, y, r.W, LineHeight(tf)}, tf, Black, a)
		y += LineHeight(tf) - 4
	}
	y += 8
	if t.artist != "" && y+LineHeight(af) <= r.Bottom()+8 {
		c.TextIn(t.artist, Rect{r.X, y, r.W, LineHeight(af)}, af, Dark, a)
		y += LineHeight(af)
	}
	if t.album != "" && y+LineHeight(sf) <= r.Bottom()+8 {
		c.TextIn(t.album, Rect{r.X, y, r.W, LineHeight(sf)}, sf, Mid, a)
		y += LineHeight(sf)
	}
	return y
}

// transportRow draws previous, play/pause and next around the middle of box, and (optionally)
// volume buttons at its two ends.
func transportRow(c *Canvas, box Rect, playR int, withVolume bool, s *Scene, hits *[]HitZone) {
	p := s.Player
	cx, cy := box.CX(), box.CY()
	sideR := playR * 3 / 4
	d := playR + sideR + 44
	narrow := withVolume && box.W < 700 // landscape column: keep the volume buttons clear of previous/next
	if narrow {
		d = playR + sideR + 26
	}
	roundButton(c, cx-d, cy, sideR, "prev", IconPrev, float64(sideR)*1.05, btnPlain, p.Active, s, hits)
	if p.Playing {
		roundButton(c, cx, cy, playR, "toggle", IconPause, float64(playR)*0.95, btnFilled, true, s, hits)
	} else {
		roundButton(c, cx, cy, playR, "toggle", IconPlay, float64(playR)*0.95, btnFilled, true, s, hits)
	}
	roundButton(c, cx+d, cy, sideR, "next", IconNext, float64(sideR)*1.05, btnPlain, p.Active, s, hits)
	if withVolume {
		hasVol := p.Active && p.Volume >= 0
		vr := 40
		if narrow {
			vr = 36
		}
		roundButton(c, box.X+vr+6, cy, vr, "vol_down", IconVolDown, 44, btnOutlined, hasVol, s, hits)
		roundButton(c, box.Right()-vr-6, cy, vr, "vol_up", IconVolUp, 44, btnOutlined, hasVol, s, hits)
	}
}

func IconChevL(c *Canvas, cx, cy, s float64, v uint8) {
	c.Poly(v, cx+0.18*s, cy-0.46*s, cx+0.32*s, cy-0.32*s, cx-0.04*s, cy, cx+0.32*s, cy+0.32*s, cx+0.18*s, cy+0.46*s, cx-0.32*s, cy)
}

func IconChevR(c *Canvas, cx, cy, s float64, v uint8) {
	c.Poly(v, cx-0.18*s, cy-0.46*s, cx-0.32*s, cy-0.32*s, cx+0.04*s, cy, cx-0.32*s, cy+0.32*s, cx-0.18*s, cy+0.46*s, cx+0.32*s, cy)
}
