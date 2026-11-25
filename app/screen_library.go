package main

import (
	"fmt"
	"time"
)

// LibSection is one tab of the library browser.
type LibSection int

const (
	LibPlaylists LibSection = iota
	LibAlbums
	LibLiked
	LibRecent
	LibDevices
	libSections = 5
)

var libLabels = [libSections]string{"Playlists", "Albums", "Liked", "Recent", "Devices"}

// LibList is one section's rows as loaded from Spotify.
type LibList struct {
	Items   []LibItem
	Loading bool
	Loaded  bool
	Err     string
	At      time.Time
}

type LibView struct {
	Section LibSection
	Page    int
	List    LibList
}

// drawLibrary lists playlists, albums, liked songs, recent plays and devices, a page at a time.
// Pages instead of scrolling: every page turn is one clean screen update.
func drawLibrary(c *Canvas, l layout, s *Scene, hits *[]HitZone) {
	a, lv := l.Content, s.Library

	seg := Rect{a.X, a.Y, a.W, 80}
	segmented(c, seg, libLabels[:], int(lv.Section), true, func(i int) string { return fmt.Sprintf("lib_sec:%d", i) }, hits)

	pagerH := 96
	pager := Rect{a.X, a.Bottom() - pagerH, a.W, pagerH}
	top := seg.Bottom() + 20
	list := Rect{a.X, top, a.W, pager.Y - 10 - top}

	rowH := 122
	rows := max(1, list.H/rowH)
	items := lv.List.Items
	pages := max(1, (len(items)+rows-1)/rows)
	page := clampInt(lv.Page, 0, pages-1)
	o := s.out()
	o.LibRows, o.LibPage, o.LibPages = rows, page, pages

	tf, sf, mf := Face(Bold, 38), Face(Regular, 28), Face(Light, 36)
	say := func(msg string, ink uint8) {
		y := list.Y + 24
		for _, ln := range Wrap(mf, msg, list.W-12, 4) {
			c.TextIn(ln, Rect{list.X + 6, y, list.W, LineHeight(mf)}, mf, ink, Left)
			y += LineHeight(mf)
		}
	}
	switch {
	case lv.List.Loading && len(items) == 0:
		say("Loading…", Mid)
	case lv.List.Err != "":
		say(lv.List.Err, Dark)
	case len(items) == 0 && lv.Section == LibDevices:
		say("No Spotify device is on. Open Spotify on your phone or computer.", Mid)
	case len(items) == 0:
		say("Nothing here yet.", Mid)
	}

	badgeW := 156
	for r := 0; r < rows; r++ {
		idx := page*rows + r
		if idx >= len(items) {
			break
		}
		it := items[idx]
		row := Rect{list.X, list.Y + r*rowH, list.W, rowH}
		textW := row.W - badgeW - 40
		c.TextIn(it.Title, Rect{row.X + 4, row.Y + 20, textW, LineHeight(tf)}, tf, Black, Left)
		if it.Sub != "" {
			c.TextIn(it.Sub, Rect{row.X + 4, row.Y + 20 + LineHeight(tf) - 2, textW, LineHeight(sf)}, sf, Mid, Left)
		}
		if it.Kind == KindDevice {
			badge := Rect{row.Right() - badgeW, row.CY() - 33, badgeW, 66}
			if it.Active {
				pillButton(c, badge, "Playing", "", true, true, hits)
			} else {
				pillButton(c, badge, "Switch", "", false, true, hits)
			}
		} else {
			cx, cy := float64(row.Right()-40), float64(row.CY())
			c.Circle(cx, cy, 36, Black)
			IconPlay(c, cx+2, cy, 34, White)
		}
		if r < rows-1 && idx+1 < len(items) {
			hairline(c, row.X, row.Bottom()-2, row.W, Pale)
		}
		*hits = append(*hits, HitZone{row, fmt.Sprintf("lib:%d", idx)})
	}

	// pager: previous, page number, next, reload
	cy := pager.CY()
	roundButton(c, pager.X+46, cy, 44, "lib_prev", IconChevL, 40, btnOutlined, page > 0, s, hits)
	roundButton(c, pager.Right()-46-120, cy, 44, "lib_next", IconChevR, 40, btnOutlined, page < pages-1, s, hits)
	roundButton(c, pager.Right()-46, cy, 44, "lib_reload", IconRotate, 42, btnOutlined, true, s, hits)
	pf, nf := Face(Bold, 32), Face(Regular, 24)
	mid := Rect{pager.X + 100, pager.Y, pager.W - 100 - 240, pagerH}
	c.TextIn(fmt.Sprintf("Page %d of %d", page+1, pages), Rect{mid.X, mid.Y + 16, mid.W, LineHeight(pf)}, pf, Black, Center)
	if len(items) > 0 {
		c.TextIn(fmt.Sprintf("%d items", len(items)), Rect{mid.X, mid.Y + 16 + LineHeight(pf) - 4, mid.W, LineHeight(nf)}, nf, Mid, Center)
	}
}
