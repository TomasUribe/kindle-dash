package main

import (
	"fmt"
	"time"

	"golang.org/x/image/font"
)

var lyricSizes = []int{34, 40, 48, 58}

// LyricsView is everything the lyrics screen needs besides the player.
type LyricsView struct {
	L         *Lyrics
	Loading   bool
	Err       string
	Disabled  bool
	Page      int  // the page to show when not following the song
	Auto      bool // follow the song
	Size      int
	Highlight bool // mark the line being sung (costs a small screen update every line)
}

type vline struct {
	text string
	idx  int // which lyric line it belongs to; -1 for a gap between verses
}

// layoutLyrics wraps every lyric line to the width, keeping track of where each came from.
func layoutLyrics(l *Lyrics, f font.Face, maxW int) []vline {
	var out []vline
	for i, ln := range l.Lines {
		if ln.Text == "" {
			out = append(out, vline{idx: -1})
			continue
		}
		for _, part := range Wrap(f, ln.Text, maxW, 4) {
			out = append(out, vline{part, i})
		}
	}
	return out
}

// lyricsLead is how early a line is shown: the panel needs a moment to redraw, so the
// highlight and the page turn run ahead of the singer.
func lyricsLead(cfg *Config) time.Duration {
	return time.Duration(cfg.LyricsLeadSeconds * float64(time.Second))
}

// lyricsLayout is shared by drawing and by timing: it says how the lyrics break into pages.
type lyricsPlan struct {
	vl      []vline
	perPage int
	pages   int
}

func planLyrics(l *Lyrics, size int, area Rect) lyricsPlan {
	f, fb := Face(Regular, size), Face(Bold, size)
	lh := LineHeight(f) + 10
	pp := max(1, area.H/lh)
	vl := layoutLyrics(l, fb, area.W) // wrapped with the bold face so a highlight never re-wraps a line
	return lyricsPlan{vl: vl, perPage: pp, pages: max(1, (len(vl)+pp-1)/pp)}
}

// lyricsPageLead is how long before a page's first line is sung that the page comes up. It is shorter
// than the highlight lead on purpose: a page turn hides the old lines, so it must not happen while the
// last line of the old page is still being sung.
func lyricsPageLead(cfg *Config) time.Duration {
	return min(lyricsLead(cfg), time.Second)
}

// firstLineFrom is the lyric line of the first visible row at or after row i, or -1.
func (lp lyricsPlan) firstLineFrom(i int) int {
	for ; i < len(lp.vl); i++ {
		if lp.vl[i].idx >= 0 {
			return lp.vl[i].idx
		}
	}
	return -1
}

// lastLineOn is the last lyric line with a row on the page.
func (lp lyricsPlan) lastLineOn(page int) int {
	last := -1
	for i := page * lp.perPage; i < len(lp.vl) && i < (page+1)*lp.perPage; i++ {
		if lp.vl[i].idx >= 0 {
			last = lp.vl[i].idx
		}
	}
	return last
}

const never = time.Duration(1 << 62)

// pageTurns says, for pages 1.., the song time at which each comes up while following the song.
// A page turns shortly before its first line is sung, but never before 70% of the time the last line
// of the page before it has to be sung (that is the gap until the next line starts).
func (lp lyricsPlan) pageTurns(l *Lyrics, pageLead time.Duration) []time.Duration {
	var turns []time.Duration
	prevAt := time.Duration(0)
	for p := 1; p < lp.pages; p++ {
		line := lp.firstLineFrom(p * lp.perPage)
		at := never
		if line >= 0 && line < len(l.Lines) {
			at = l.Lines[line].At - pageLead
			if line > 0 {
				prev := l.Lines[line-1].At
				at = max(at, prev+(l.Lines[line].At-prev)*7/10)
			}
		}
		at = max(at, prevAt)
		turns = append(turns, at)
		prevAt = at
	}
	return turns
}

// pageAt is the page showing at song time pos, given the turn times of pages 1...
func pageAt(turns []time.Duration, pos time.Duration) int {
	page := 0
	for i, at := range turns {
		if pos < at {
			break
		}
		page = i + 1
	}
	return page
}

// drawLyrics shows the cover and controls on one side and the lyrics, a page at a time, on the other.
func drawLyrics(c *Canvas, l layout, s *Scene, hits *[]HitZone) {
	a, lv, p := l.Content, s.Lyrics, s.Player
	chipsH := 84

	var player, text, chips Rect
	if l.Land {
		leftW := 560
		player = Rect{a.X, a.Y, leftW, a.H}
		rx := a.X + leftW + 56
		chips = Rect{rx, a.Bottom() - chipsH, a.Right() - rx, chipsH}
		text = Rect{rx, a.Y, a.Right() - rx, chips.Y - 18 - a.Y}
		drawLyricsPlayerSide(c, player, s, hits)
	} else {
		stripH := 210
		player = Rect{a.X, a.Y, a.W, stripH + 74}
		chips = Rect{a.X, a.Bottom() - chipsH, a.W, chipsH}
		top := a.Y + stripH + 74 + 26
		text = Rect{a.X, top, a.W, chips.Y - 18 - top}
		drawLyricsPlayerStrip(c, Rect{a.X, a.Y, a.W, stripH}, s, hits)
	}

	size := lv.Size
	if size == 0 {
		size = lyricSizes[1]
	}
	in := Rect{text.X + 38, text.Y, text.W - 38, text.H}
	havePage := lv.L != nil && lv.L.Found && !lv.L.Instrumental && len(lv.L.Lines) > 0
	pages, page, cur := 1, 0, -1
	var plan lyricsPlan
	if havePage {
		plan = planLyrics(lv.L, size, in)
		pages = plan.pages
		pos := p.Expected(s.Now)
		cur = lv.L.CurrentIndex(pos + lyricsLead(s.Cfg))
		if lv.Auto && lv.L.Synced {
			turns := plan.pageTurns(lv.L, lyricsPageLead(s.Cfg))
			page = pageAt(turns, pos)
			s.out().LyrTurns = turns
			// the highlight runs ahead of the singer, but not onto a page that is not showing yet
			cur = min(cur, plan.lastLineOn(page))
		} else {
			page = clampInt(lv.Page, 0, pages-1)
		}
	}
	o := s.out()
	o.LyrPage, o.LyrPages, o.LyrLine = page, pages, cur

	msg := ""
	switch {
	case lv.Disabled:
		msg = "Lyrics are switched off in config.json."
	case !p.Active:
		msg = "Play something to see its lyrics."
	case lv.Loading:
		msg = "Looking for lyrics…"
	case lv.Err != "":
		msg = "Could not load lyrics: " + lv.Err
	case lv.L != nil && lv.L.Instrumental:
		msg = "Instrumental — no lyrics."
	case !havePage:
		msg = "No lyrics found for this track."
	}
	if msg != "" {
		mf := Face(Medium, 36)
		y := text.Y + 20
		for _, ln := range Wrap(mf, msg, text.W-40, 4) {
			c.TextIn(ln, Rect{text.X + 6, y, text.W, LineHeight(mf)}, mf, Mid, Left)
			y += LineHeight(mf)
		}
	} else {
		f, fb := Face(Regular, size), Face(Bold, size)
		lh := LineHeight(f) + 10
		start := page * plan.perPage
		first, last := -1, -1
		for i := 0; i < plan.perPage && start+i < len(plan.vl); i++ {
			v := plan.vl[start+i]
			if v.text == "" {
				continue
			}
			face, ink := f, Black
			if lv.Highlight && lv.L.Synced && cur >= 0 {
				if v.idx == cur {
					face = fb
					if first < 0 {
						first = i
					}
					last = i
				} else {
					ink = Dark
				}
			}
			c.TextIn(v.text, Rect{in.X, in.Y + i*lh, in.W, LineHeight(face)}, face, ink, Left)
		}
		if first >= 0 { // a bar beside the line that is about to be sung
			c.RoundRect(Rect{text.X + 4, in.Y + first*lh + 4, 8, (last-first+1)*lh - 8}, 4, Black)
		}
	}

	// controls: previous page, page number, next page, follow, size, line marker
	synced := havePage && lv.L.Synced
	weights := []float64{1, 1.5, 1, 2, 1.2, 1.5}
	gap := 14
	unit := float64(chips.W-gap*(len(weights)-1)) / 8.2
	x := chips.X
	box := func(i int) Rect {
		w := int(unit * weights[i])
		r := Rect{x, chips.Y, w, chips.H}
		x += w + gap
		return r
	}
	pillIconButton(c, box(0), IconChevL, 38, "lyr_prev", havePage && page > 0, hits)
	pr := box(1)
	if havePage {
		pf := Face(Medium, 28)
		c.TextIn(fmt.Sprintf("%d / %d", page+1, pages), Rect{pr.X, pr.CY() - LineHeight(pf)/2, pr.W, LineHeight(pf)}, pf, Dark, Center)
	}
	pillIconButton(c, box(2), IconChevR, 38, "lyr_next", havePage && page < pages-1, hits)
	pillButton(c, box(3), "Follow", "lyr_auto", lv.Auto && synced, synced, hits)
	pillButton(c, box(4), "Aa", "lyr_size", false, true, hits)
	pillButton(c, box(5), "Line", "lyr_hl", lv.Highlight && synced, synced, hits)
}

// drawLyricsPlayerStrip is the cover, title and play controls across the top (portrait).
func drawLyricsPlayerStrip(c *Canvas, r Rect, s *Scene, hits *[]HitZone) {
	p := s.Player
	art := r.H
	artBox(c, Rect{r.X, r.Y, art, art}, s)
	col := Rect{r.X + art + 44, r.Y, r.W - art - 44, r.H}
	title, artist := "Nothing playing", ""
	if p.Active {
		title, artist = orDash(p.Title), p.Artist
	}
	tf, af := Face(Bold, 40), Face(Medium, 30)
	c.TextIn(title, Rect{col.X, col.Y + 4, col.W, LineHeight(tf)}, tf, Black, Left)
	c.TextIn(artist, Rect{col.X, col.Y + 4 + LineHeight(tf) - 4, col.W, LineHeight(af)}, af, Dark, Left)
	transportRow(c, Rect{col.X, r.Bottom() - 104, col.W, 104}, 42, false, s, hits)
	prog := Rect{r.X, r.Bottom() + 14, r.W, 30}
	seekBar(c, prog, s, hits)
	progressTimes(c, Rect{prog.X, prog.Bottom() + 2, prog.W, 26}, s)
}

// drawLyricsPlayerSide is the same player stacked in a column (landscape).
func drawLyricsPlayerSide(c *Canvas, r Rect, s *Scene, hits *[]HitZone) {
	p := s.Player
	t := playerTexts_(s)
	t.album = ""
	ctlH, progH, textH := 112, 64, 112
	art := min(r.W, r.H-ctlH-progH-textH-30)
	artBox(c, Rect{r.CX() - art/2, r.Y, art, art}, s)
	text := Rect{r.X, r.Y + art + 22, r.W, textH}
	heroText(c, text, t, Center, 38, 28, 26, false)
	prog := Rect{r.X, r.Bottom() - ctlH - progH, r.W, 30}
	seekBar(c, prog, s, hits)
	progressTimes(c, Rect{prog.X, prog.Bottom() + 2, prog.W, 26}, s)
	transportRow(c, Rect{r.X, r.Bottom() - ctlH, r.W, ctlH}, 46, false, s, hits)
	_ = p
}
