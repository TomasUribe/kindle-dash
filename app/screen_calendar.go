package main

import (
	"fmt"
	"strings"
	"time"
)

// drawCalendar shows the month with dots on busy days, and the agenda.
func drawCalendar(c *Canvas, l layout, s *Scene, hits *[]HitZone) {
	a := l.Content
	st := agendaStyle{title: 40, when: 29, sub: 26, hdr: 25, pad: 12}
	if l.Land {
		mw := 780
		drawMonth(c, Rect{a.X, a.Y, mw, a.H}, s)
		drawAgenda(c, Rect{a.X + mw + 56, a.Y, a.W - mw - 56, a.H}, s, st)
		return
	}
	mh := 560
	drawMonth(c, Rect{a.X, a.Y, a.W, mh}, s)
	drawAgenda(c, Rect{a.X, a.Y + mh + 30, a.W, a.H - mh - 30}, s, st)
}

func drawMonth(c *Canvas, r Rect, s *Scene) {
	now := s.Now.In(s.Cfg.Loc)
	loc := s.Cfg.Loc
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)

	tf := Face(Bold, 46)
	c.TextIn(first.Format("January"), Rect{r.X, r.Y, r.W, LineHeight(tf)}, tf, Black, Left)
	yf := Face(Light, 46)
	c.TextIn(first.Format("2006"), Rect{r.X + TextWidth(tf, first.Format("January")) + 16, r.Y, r.W, LineHeight(yf)}, yf, Mid, Left)

	hy := r.Y + LineHeight(tf) + 12
	colW := r.W / 7
	startDay := 1
	names := []string{"M", "T", "W", "T", "F", "S", "S"}
	if !s.Cfg.MondayFirst() {
		names = []string{"S", "M", "T", "W", "T", "F", "S"}
		startDay = 0
	}
	wf := Face(Medium, 24)
	for i, n := range names {
		c.TextIn(n, Rect{r.X + i*colW, hy, colW, LineHeight(wf)}, wf, Mid, Center)
	}
	hairline(c, r.X, hy+LineHeight(wf)+8, r.W, Pale)

	// dots: days that have at least one event
	busy := map[string]int{}
	for _, e := range s.Agenda.Events {
		end := maxTime(e.End, e.Start.Add(time.Second))
		for d := midnight(e.Start, loc); d.Before(end); d = d.AddDate(0, 0, 1) {
			busy[d.Format("20060102")]++
		}
	}

	offset := (int(first.Weekday()) - startDay + 7) % 7
	daysIn := first.AddDate(0, 1, -1).Day()
	rows := (offset + daysIn + 6) / 7
	gy := hy + LineHeight(wf) + 18
	rowH := (r.Bottom() - gy) / rows
	nf, nb := Face(Regular, 34), Face(Bold, 34)
	for d := 1; d <= daysIn; d++ {
		idx := offset + d - 1
		cell := Rect{r.X + (idx%7)*colW, gy + (idx/7)*rowH, colW, rowH}
		date := time.Date(now.Year(), now.Month(), d, 0, 0, 0, 0, loc)
		isToday := d == now.Day()
		cx, cy := float64(cell.CX()), float64(cell.Y)+float64(rowH)*0.40
		ink, f := Black, nf
		if isToday {
			c.Circle(cx, cy, float64(min(colW, rowH))*0.36, Black)
			ink, f = White, nb
		} else if date.Before(midnight(now, loc)) {
			ink = Mid
		}
		c.TextIn(fmt.Sprint(d), Rect{cell.X, int(cy) - LineHeight(f)/2, cell.W, LineHeight(f)}, f, ink, Center)
		if n := busy[date.Format("20060102")]; n > 0 {
			dots := min(n, 3)
			for k := 0; k < dots; k++ {
				dx := cx + (float64(k)-float64(dots-1)/2)*14
				c.Circle(dx, float64(cell.Y)+float64(rowH)*0.90, 4.5, Black)
			}
		}
	}
}

// --- Agenda --------------------------------------------------------------------------

type agendaStyle struct{ title, when, sub, hdr, pad int }

type block struct {
	kind   int // 0 heading, 1 row, 2 note
	h      int
	day    int
	row    Row
	label  string
	sub    string
	inPast bool
	lines  []string
}

func drawAgenda(c *Canvas, r Rect, s *Scene, st agendaStyle) {
	now := s.Now.In(s.Cfg.Loc)
	tf, wf, sf, hf := Face(Bold, st.title), Face(Medium, st.when), Face(Regular, st.sub), Face(Bold, st.hdr)
	noteF := Face(Light, st.title-4)

	timeSample := "00:00–00:00"
	if !s.Cfg.Use24h() {
		timeSample = "00:00pm–00:00pm"
	}
	timeW := TextWidth(wf, timeSample) + 18
	titleW := r.W - timeW - 22

	plan := func(dropPast bool) ([]block, bool) {
		var blocks []block
		anyPast := false
		for di, day := range s.Agenda.Days {
			blocks = append(blocks, block{kind: 0, h: LineHeight(hf) + 26, day: di, label: strings.ToUpper(day.Label), sub: day.Sub})
			shown, skipped := 0, 0
			for _, row := range day.Rows {
				past := !row.AllDay && !row.End.After(now)
				anyPast = anyPast || past
				if dropPast && past {
					skipped++
					continue
				}
				lines := Wrap(tf, row.Title, titleW, 2)
				if len(lines) == 0 {
					lines = []string{row.Title}
				}
				h := 2*st.pad + len(lines)*(LineHeight(tf)-6) + 6
				sub := subLine(row)
				if sub != "" {
					h += LineHeight(sf) + 1
				}
				blocks = append(blocks, block{kind: 1, h: h, day: di, row: row, sub: sub, inPast: past, lines: lines})
				shown++
			}
			if shown == 0 {
				note := "Nothing scheduled"
				if skipped > 0 {
					note = "Nothing more today"
				}
				blocks = append(blocks, block{kind: 2, h: LineHeight(noteF) + 20, day: di, label: note})
			}
		}
		return blocks, anyPast
	}
	fits := func(blocks []block, limit int) int {
		y, n := 0, 0
		for _, b := range blocks {
			if y+b.h > limit {
				break
			}
			y += b.h
			n++
		}
		return n
	}
	sum := func(blocks []block) int {
		t := 0
		for _, b := range blocks {
			t += b.h
		}
		return t
	}

	blocks, anyPast := plan(false)
	moreH := 44
	if sum(blocks) > r.H && anyPast {
		blocks, _ = plan(true) // upcoming events matter more than finished ones
	}
	limit := r.H
	if sum(blocks) > r.H {
		limit = r.H - moreH
	}
	n := fits(blocks, limit)
	if n > 0 && blocks[n-1].kind == 0 { // never end on a lone heading
		n--
	}

	y := r.Y
	for i := 0; i < n; i++ {
		b := blocks[i]
		switch b.kind {
		case 0:
			c.Text(b.label, r.X+2, y+4+Ascent(hf), hf, Black)
			c.TextIn(b.sub, Rect{r.X, y + 5, r.W - 2, LineHeight(hf)}, Face(Regular, st.hdr+1), Mid, Right)
			hairline(c, r.X, y+b.h-9, r.W, Soft)
		case 2:
			c.TextIn(b.label, Rect{r.X + 2, y + 10, r.W, LineHeight(noteF)}, noteF, Mid, Left)
		case 1:
			row := b.row
			current := !row.AllDay && !row.Start.After(now) && row.End.After(now)
			ink, subInk, weight := Black, Dark, Bold
			if current {
				c.RoundRect(Rect{r.X - 10, y + 3, r.W + 20, b.h - 6}, 22, Black)
				ink, subInk = White, Soft
			} else if b.inPast {
				ink, subInk, weight = Mid, Soft, Regular
			}
			tfx := Face(weight, st.title)
			c.TextIn(row.When, Rect{r.X + 6, y + st.pad + 4, timeW, LineHeight(wf)}, wf, ink, Left)
			tx := Rect{r.X + 6 + timeW + 8, y + st.pad - 3, titleW, LineHeight(tfx)}
			step := LineHeight(tfx) - 6
			for li, ln := range b.lines {
				c.TextIn(ln, Rect{tx.X, tx.Y + li*step, tx.W, LineHeight(tfx)}, tfx, ink, Left)
			}
			if b.sub != "" {
				c.TextIn(b.sub, Rect{tx.X, tx.Y + len(b.lines)*step + 3, tx.W, LineHeight(sf)}, sf, subInk, Left)
			}
		}
		y += b.h
	}
	if n < len(blocks) {
		left := 0
		for _, b := range blocks[n:] {
			if b.kind == 1 {
				left++
			}
		}
		if left > 0 {
			c.TextIn(fmt.Sprintf("+%d more", left), Rect{r.X, r.Bottom() - moreH + 8, r.W, moreH - 8}, Face(Bold, st.sub+3), Dark, Right)
		}
	}
}

func subLine(r Row) string {
	var parts []string
	if r.Location != "" {
		parts = append(parts, r.Location)
	}
	if r.Cal != "" {
		parts = append(parts, r.Cal)
	}
	return strings.Join(parts, "  ·  ")
}
