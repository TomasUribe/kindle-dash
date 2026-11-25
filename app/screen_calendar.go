package main

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/image/font"
)

// CalView is what the Calendar screen is showing besides the data: which month, which day, which event.
type CalView struct {
	Off    int       // months away from the current month
	Day    time.Time // the selected day (local midnight); zero when none
	Page   int       // page of the event list
	Detail *Row      // the event that is open, if any
	DPage  int       // page of the open event's text
}

func (v CalView) month(now time.Time, loc *time.Location) time.Time {
	n := now.In(loc)
	return time.Date(n.Year(), n.Month()+time.Month(v.Off), 1, 0, 0, 0, 0, loc)
}

// monthsBetween is how many months date d is from the month of now.
func monthsBetween(now, d time.Time) int {
	return (d.Year()-now.Year())*12 + int(d.Month()) - int(now.Month())
}

// drawCalendar shows a month you can move through, and below it the agenda, a day, or a month's events.
// Tapping an event opens it full screen.
func drawCalendar(c *Canvas, l layout, s *Scene, hits *[]HitZone) {
	a := l.Content
	if s.Cal.Detail != nil {
		drawEventDetail(c, a, s, hits)
		return
	}
	st := agendaStyle{title: 40, when: 29, sub: 26, hdr: 25, pad: 12}
	var monthR, listR Rect
	if l.Land {
		mw := 780
		monthR = Rect{a.X, a.Y, mw, a.H}
		listR = Rect{a.X + mw + 56, a.Y, a.W - mw - 56, a.H}
	} else {
		mh := 560
		monthR = Rect{a.X, a.Y, a.W, mh}
		listR = Rect{a.X, a.Y + mh + 30, a.W, a.H - mh - 30}
	}
	month := s.Cal.month(s.Now, s.Cfg.Loc)
	drawMonth(c, monthR, s, month, hits)

	o := s.out()
	var rows []Row
	switch {
	case !s.Cal.Day.IsZero():
		o.CalPage, o.CalPages = drawDayPanel(c, listR, s, st, hits, &rows)
	case s.Cal.Off == 0:
		drawAgendaDays(c, listR, s.Agenda.Days, s, st, hits, &rows)
	default:
		o.CalPage, o.CalPages = drawMonthList(c, listR, s, st, month, hits, &rows)
	}
	o.CalRows = rows
}

func drawMonth(c *Canvas, r Rect, s *Scene, month time.Time, hits *[]HitZone) {
	loc := s.Cfg.Loc
	now := s.Now.In(loc)
	today := midnight(now, loc)

	tf := Face(Bold, 46)
	c.TextIn(month.Format("January"), Rect{r.X, r.Y, r.W, LineHeight(tf)}, tf, Black, Left)
	yf := Face(Light, 46)
	c.TextIn(month.Format("2006"), Rect{r.X + TextWidth(tf, month.Format("January")) + 16, r.Y, r.W, LineHeight(yf)}, yf, Mid, Left)

	// month controls, to the right of the title
	bh := 64
	by := r.Y + (LineHeight(tf)-bh)/2 + 4
	next := Rect{r.Right() - 84, by, 84, bh}
	prev := Rect{next.X - 10 - 84, by, 84, bh}
	tod := Rect{prev.X - 12 - 112, by, 112, bh}
	pillIconButton(c, prev, IconChevL, 36, "cal_prev", s.Cal.Off > -s.Cfg.CalendarBrowseBack, hits)
	pillIconButton(c, next, IconChevR, 36, "cal_next", s.Cal.Off < s.Cfg.CalendarBrowseAhead, hits)
	pillButton(c, tod, "Today", "cal_today", false, s.Cal.Off != 0 || !s.Cal.Day.IsZero(), hits)

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
		if !e.Start.Before(month.AddDate(0, 1, 0)) || !end.After(month) {
			continue // not in this month
		}
		for d := midnight(e.Start, loc); d.Before(end); d = d.AddDate(0, 0, 1) {
			busy[d.Format("20060102")]++
		}
	}

	offset := (int(month.Weekday()) - startDay + 7) % 7
	daysIn := month.AddDate(0, 1, -1).Day()
	rows := (offset + daysIn + 6) / 7
	gy := hy + LineHeight(wf) + 18
	rowH := (r.Bottom() - gy) / rows
	nf, nb := Face(Regular, 34), Face(Bold, 34)
	sel := ""
	if !s.Cal.Day.IsZero() {
		sel = s.Cal.Day.Format("20060102")
	}
	for d := 1; d <= daysIn; d++ {
		idx := offset + d - 1
		cell := Rect{r.X + (idx%7)*colW, gy + (idx/7)*rowH, colW, rowH}
		date := time.Date(month.Year(), month.Month(), d, 0, 0, 0, 0, loc)
		key := date.Format("20060102")
		isToday := date.Equal(today)
		selected := key == sel
		cx, cy := float64(cell.CX()), float64(cell.Y)+float64(rowH)*0.40
		radius := float64(min(colW, rowH)) * 0.36
		ink, f := Black, nf
		switch {
		case isToday:
			if selected { // a ring around the filled circle
				c.Circle(cx, cy, radius+8, Black)
				c.Circle(cx, cy, radius+4, White)
			}
			c.Circle(cx, cy, radius, Black)
			ink, f = White, nb
		case selected:
			c.Circle(cx, cy, radius, Black)
			c.Circle(cx, cy, radius-4, White)
			f = nb
		case date.Before(today):
			ink = Mid
		}
		c.TextIn(fmt.Sprint(d), Rect{cell.X, int(cy) - LineHeight(f)/2, cell.W, LineHeight(f)}, f, ink, Center)
		if n := busy[key]; n > 0 {
			dots := min(n, 3)
			for k := 0; k < dots; k++ {
				dx := cx + (float64(k)-float64(dots-1)/2)*14
				c.Circle(dx, float64(cell.Y)+float64(rowH)*0.90, 4.5, Black)
			}
		}
		*hits = append(*hits, HitZone{cell, "cal_day:" + key})
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

// agendaPainter measures and draws the headings, rows and notes of an event list.
type agendaPainter struct {
	st             agendaStyle
	now            time.Time
	tf, wf, sf, hf font.Face
	noteF          font.Face
	timeW, titleW  int
}

func newAgendaPainter(s *Scene, st agendaStyle, width int) *agendaPainter {
	p := &agendaPainter{st: st, now: s.Now.In(s.Cfg.Loc)}
	p.tf, p.wf, p.sf, p.hf = Face(Bold, st.title), Face(Medium, st.when), Face(Regular, st.sub), Face(Bold, st.hdr)
	p.noteF = Face(Light, st.title-4)
	timeSample := "00:00–00:00"
	if !s.Cfg.Use24h() {
		timeSample = "00:00pm–00:00pm"
	}
	p.timeW = TextWidth(p.wf, timeSample) + 18
	p.titleW = width - p.timeW - 22
	return p
}

func (p *agendaPainter) heading(day Day, di int) block {
	return block{kind: 0, h: LineHeight(p.hf) + 26, day: di, label: strings.ToUpper(day.Label), sub: day.Sub}
}

func (p *agendaPainter) note(msg string, di int) block {
	return block{kind: 2, h: LineHeight(p.noteF) + 20, day: di, label: msg}
}

func (p *agendaPainter) rowBlock(row Row, di int) block {
	past := !row.AllDay && !row.End.After(p.now)
	lines := Wrap(p.tf, row.Title, p.titleW, 2)
	if len(lines) == 0 {
		lines = []string{row.Title}
	}
	h := 2*p.st.pad + len(lines)*(LineHeight(p.tf)-6) + 6
	sub := subLine(row)
	if sub != "" {
		h += LineHeight(p.sf) + 1
	}
	return block{kind: 1, h: h, day: di, row: row, sub: sub, inPast: past, lines: lines}
}

// paint draws one block at y. Rows become tappable ("cal_ev:N", N counting the rows drawn so far) when
// hits and rows are given.
func (p *agendaPainter) paint(c *Canvas, r Rect, y int, b block, hits *[]HitZone, rows *[]Row) {
	st := p.st
	switch b.kind {
	case 0:
		c.Text(b.label, r.X+2, y+4+Ascent(p.hf), p.hf, Black)
		c.TextIn(b.sub, Rect{r.X, y + 5, r.W - 2, LineHeight(p.hf)}, Face(Regular, st.hdr+1), Mid, Right)
		hairline(c, r.X, y+b.h-9, r.W, Soft)
	case 2:
		c.TextIn(b.label, Rect{r.X + 2, y + 10, r.W, LineHeight(p.noteF)}, p.noteF, Mid, Left)
	case 1:
		row := b.row
		current := !row.AllDay && !row.Start.After(p.now) && row.End.After(p.now)
		ink, subInk, weight := Black, Dark, Bold
		if current {
			c.RoundRect(Rect{r.X - 10, y + 3, r.W + 20, b.h - 6}, 22, Black)
			ink, subInk = White, Soft
		} else if b.inPast {
			ink, subInk, weight = Mid, Soft, Regular
		}
		tfx := Face(weight, st.title)
		c.TextIn(row.When, Rect{r.X + 6, y + st.pad + 4, p.timeW, LineHeight(p.wf)}, p.wf, ink, Left)
		tx := Rect{r.X + 6 + p.timeW + 8, y + st.pad - 3, p.titleW, LineHeight(tfx)}
		step := LineHeight(tfx) - 6
		for li, ln := range b.lines {
			c.TextIn(ln, Rect{tx.X, tx.Y + li*step, tx.W, LineHeight(tfx)}, tfx, ink, Left)
		}
		if b.sub != "" {
			c.TextIn(b.sub, Rect{tx.X, tx.Y + len(b.lines)*step + 3, tx.W, LineHeight(p.sf)}, p.sf, subInk, Left)
		}
		if hits != nil && rows != nil {
			*hits = append(*hits, HitZone{Rect{r.X - 10, y, r.W + 20, b.h}, fmt.Sprintf("cal_ev:%d", len(*rows))})
			*rows = append(*rows, row)
		}
	}
}

// drawAgenda is the coming days, as on the Home screen (not tappable).
func drawAgenda(c *Canvas, r Rect, s *Scene, st agendaStyle) {
	drawAgendaDays(c, r, s.Agenda.Days, s, st, nil, nil)
}

// drawAgendaDays lists the given days as far as they fit, with "+N more" when they do not.
func drawAgendaDays(c *Canvas, r Rect, days []Day, s *Scene, st agendaStyle, hits *[]HitZone, rows *[]Row) {
	p := newAgendaPainter(s, st, r.W)
	now := p.now

	plan := func(dropPast bool) ([]block, bool) {
		var blocks []block
		anyPast := false
		for di, day := range days {
			blocks = append(blocks, p.heading(day, di))
			shown, skipped := 0, 0
			for _, row := range day.Rows {
				past := !row.AllDay && !row.End.After(now)
				anyPast = anyPast || past
				if dropPast && past {
					skipped++
					continue
				}
				blocks = append(blocks, p.rowBlock(row, di))
				shown++
			}
			if shown == 0 {
				msg := "Nothing scheduled"
				if skipped > 0 {
					msg = "Nothing more today"
				}
				blocks = append(blocks, p.note(msg, di))
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
		p.paint(c, r, y, blocks[i], hits, rows)
		y += blocks[i].h
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

// --- paged lists: a day, or a whole month --------------------------------------------------

const pagerH = 92

// paginate splits blocks into pages of at most h pixels. A page never ends on a lone heading.
func paginate(blocks []block, h int) [][]block {
	var pages [][]block
	var cur []block
	y := 0
	for _, b := range blocks {
		if y+b.h > h && len(cur) > 0 {
			var carry []block
			if cur[len(cur)-1].kind == 0 {
				carry = []block{cur[len(cur)-1]}
				cur = cur[:len(cur)-1]
			}
			if len(cur) > 0 {
				pages = append(pages, cur)
			}
			cur = carry
			y = 0
			for _, k := range cur {
				y += k.h
			}
		}
		cur = append(cur, b)
		y += b.h
	}
	if len(cur) > 0 {
		pages = append(pages, cur)
	}
	return pages
}

// drawPager is "Page 2 of 3" with previous and next buttons, along the bottom of r.
func drawPager(c *Canvas, r Rect, page, pages int, prefix string, hits *[]HitZone) {
	y := r.Bottom() - 76
	f := Face(Medium, 28)
	c.TextIn(fmt.Sprintf("Page %d of %d", page+1, pages), Rect{r.X, y + 38 - LineHeight(f)/2, 300, LineHeight(f)}, f, Dark, Left)
	pillIconButton(c, Rect{r.Right() - 2*130 - 14, y, 130, 76}, IconChevL, 40, prefix+"_prev", page > 0, hits)
	pillIconButton(c, Rect{r.Right() - 130, y, 130, 76}, IconChevR, 40, prefix+"_next", page < pages-1, hits)
}

// drawPagedBlocks shows one page of blocks in r (with a pager when there is more than one) and
// returns the page shown and how many pages there are.
func drawPagedBlocks(c *Canvas, r Rect, p *agendaPainter, blocks []block, page int, hits *[]HitZone, rows *[]Row) (shown, pages int) {
	pg := paginate(blocks, r.H)
	if len(pg) > 1 {
		pg = paginate(blocks, r.H-pagerH)
	}
	pages = max(1, len(pg))
	shown = clampInt(page, 0, pages-1)
	if len(pg) > 0 {
		y := r.Y
		for _, b := range pg[shown] {
			p.paint(c, r, y, b, hits, rows)
			y += b.h
		}
	}
	if pages > 1 {
		drawPager(c, r, shown, pages, "cal_page", hits)
	}
	return shown, pages
}

// drawDayPanel shows the selected day: arrows to step to the day before and after, and its events.
func drawDayPanel(c *Canvas, r Rect, s *Scene, st agendaStyle, hits *[]HitZone, rows *[]Row) (page, pages int) {
	loc := s.Cfg.Loc
	now := s.Now.In(loc)
	day := s.Cal.Day
	rowsOfDay := rowsForDay(s.Agenda.Events, day, s.Cfg)

	canBack := monthsBetween(now, day.AddDate(0, 0, -1)) >= -s.Cfg.CalendarBrowseBack
	canFwd := monthsBetween(now, day.AddDate(0, 0, 1)) <= s.Cfg.CalendarBrowseAhead
	pillIconButton(c, Rect{r.X, r.Y, 100, 76}, IconChevL, 40, "cal_dprev", canBack, hits)
	pillIconButton(c, Rect{r.Right() - 100, r.Y, 100, 76}, IconChevR, 40, "cal_dnext", canFwd, hits)
	hf, sf := Face(Bold, 38), Face(Regular, 26)
	mid := Rect{r.X + 116, r.Y, r.W - 232, 76}
	c.TextIn(day.Format("Monday 2 January"), Rect{mid.X, mid.Y - 2, mid.W, LineHeight(hf)}, hf, Black, Center)
	count := "Nothing scheduled"
	switch n := len(rowsOfDay); {
	case n == 1:
		count = "1 event"
	case n > 1:
		count = fmt.Sprintf("%d events", n)
	}
	switch diff := int(day.Sub(midnight(now, loc)).Round(24*time.Hour) / (24 * time.Hour)); diff {
	case 0:
		count = "Today  ·  " + count
	case 1:
		count = "Tomorrow  ·  " + count
	case -1:
		count = "Yesterday  ·  " + count
	}
	c.TextIn(count, Rect{mid.X, mid.Y + LineHeight(hf) - 6, mid.W, LineHeight(sf)}, sf, Mid, Center)

	area := Rect{r.X, r.Y + 96, r.W, r.H - 96}
	p := newAgendaPainter(s, st, area.W)
	var blocks []block
	for _, row := range rowsOfDay {
		blocks = append(blocks, p.rowBlock(row, 0))
	}
	return drawPagedBlocks(c, area, p, blocks, s.Cal.Page, hits, rows)
}

// drawMonthList lists every day with something on in a month other than the current one.
func drawMonthList(c *Canvas, r Rect, s *Scene, st agendaStyle, month time.Time, hits *[]HitZone, rows *[]Row) (page, pages int) {
	days := buildMonthDays(s.Agenda.Events, month, s.Cfg)
	n := 0
	for _, d := range days {
		n += len(d.Rows)
	}
	f := Face(Medium, 28)
	head := fmt.Sprintf("Nothing scheduled in %s", month.Format("January"))
	switch {
	case n == 1:
		head = "1 event in " + month.Format("January")
	case n > 1:
		head = fmt.Sprintf("%d events in %s", n, month.Format("January"))
	}
	c.TextIn(head, Rect{r.X, r.Y, r.W, LineHeight(f)}, f, Dark, Left)

	area := Rect{r.X, r.Y + LineHeight(f) + 14, r.W, r.H - LineHeight(f) - 14}
	p := newAgendaPainter(s, st, area.W)
	var blocks []block
	for di, day := range days {
		blocks = append(blocks, p.heading(day, di))
		for _, row := range day.Rows {
			blocks = append(blocks, p.rowBlock(row, di))
		}
	}
	return drawPagedBlocks(c, area, p, blocks, s.Cal.Page, hits, rows)
}

// --- one event, full screen --------------------------------------------------------------

// detailWhen says on which day(s) and at what time an event is, in two lines.
func detailWhen(row Row, h24 bool) (date, clock string) {
	startDay := time.Date(row.Start.Year(), row.Start.Month(), row.Start.Day(), 0, 0, 0, 0, row.Start.Location())
	if row.AllDay {
		last := row.End.AddDate(0, 0, -1)
		if last.After(row.Start) && midnight(last, row.Start.Location()).After(startDay) {
			return row.Start.Format("Mon 2 Jan") + "  –  " + last.Format("Mon 2 Jan 2006"), "All day"
		}
		return row.Start.Format("Monday 2 January 2006"), "All day"
	}
	sameDay := !row.End.After(row.Start) || row.End.Format("20060102") == row.Start.Format("20060102") ||
		row.End.Equal(startDay.AddDate(0, 0, 1))
	if !sameDay {
		return row.Start.Format("Mon 2 Jan") + " " + clockText(row.Start, h24) + "  →  " +
			row.End.Format("Mon 2 Jan") + " " + clockText(row.End, h24), ""
	}
	if !row.End.After(row.Start) {
		return row.Start.Format("Monday 2 January 2006"), clockText(row.Start, h24)
	}
	clock = clockText(row.Start, h24) + " – " + clockText(row.End, h24)
	if d := row.End.Sub(row.Start); d >= time.Minute {
		h, m := int(d.Hours()), int(d.Minutes())%60
		switch {
		case h > 0 && m > 0:
			clock += fmt.Sprintf("   (%d h %d min)", h, m)
		case h > 0:
			clock += fmt.Sprintf("   (%d h)", h)
		default:
			clock += fmt.Sprintf("   (%d min)", m)
		}
	}
	return row.Start.Format("Monday 2 January 2006"), clock
}

// descriptionLines wraps a description paragraph by paragraph; a blank string marks a gap.
func descriptionLines(f font.Face, text string, width int) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, Wrap(f, para, width, 400)...)
	}
	return out
}

func drawEventDetail(c *Canvas, a Rect, s *Scene, hits *[]HitZone) {
	row := *s.Cal.Detail
	colW := min(a.W, 1100)
	pillButton(c, Rect{a.X, a.Y, 190, 76}, "Back", "cal_back", false, true, hits)
	y := a.Y + 76 + 30

	tf := Face(Bold, 56)
	title := row.Title
	for _, ln := range Wrap(tf, title, colW, 3) {
		c.TextIn(ln, Rect{a.X, y, colW, LineHeight(tf)}, tf, Black, Left)
		y += LineHeight(tf) - 6
	}
	y += 18

	date, clock := detailWhen(row, s.Cfg.Use24h())
	df, cf := Face(Medium, 34), Face(Bold, 40)
	c.TextIn(date, Rect{a.X, y, colW, LineHeight(df)}, df, Dark, Left)
	y += LineHeight(df) + 2
	if clock != "" {
		c.TextIn(clock, Rect{a.X, y, colW, LineHeight(cf)}, cf, Black, Left)
		y += LineHeight(cf) + 6
	}
	sf := Face(Regular, 30)
	for _, kv := range [][2]string{{"Location", row.Location}, {"Calendar", row.Cal}} {
		if kv[1] == "" {
			continue
		}
		c.TextIn(kv[0]+":  "+kv[1], Rect{a.X, y + 4, colW, LineHeight(sf)}, sf, Dark, Left)
		y += LineHeight(sf) + 6
	}
	y += 8
	hairline(c, a.X, y, colW, Soft)
	y += 22

	body := Face(Regular, 32)
	lh := LineHeight(body) + 8
	area := Rect{a.X, y, colW, a.Bottom() - y}
	o := s.out()
	if strings.TrimSpace(row.Desc) == "" {
		c.TextIn("No description", Rect{area.X, area.Y, area.W, LineHeight(body)}, Face(Light, 32), Mid, Left)
		o.CalDPage, o.CalDPages = 0, 1
		return
	}
	lines := descriptionLines(body, row.Desc, colW)
	perPage := max(1, area.H/lh)
	pages := (len(lines) + perPage - 1) / perPage
	if pages > 1 {
		perPage = max(1, (area.H-pagerH)/lh)
		pages = (len(lines) + perPage - 1) / perPage
	}
	page := clampInt(s.Cal.DPage, 0, pages-1)
	o.CalDPage, o.CalDPages = page, pages
	start := page * perPage
	ly := area.Y
	for i := start; i < len(lines) && i < start+perPage; i++ {
		if lines[i] != "" {
			c.TextIn(lines[i], Rect{area.X, ly, area.W, LineHeight(body)}, body, Black, Left)
		}
		ly += lh
	}
	if pages > 1 {
		drawPager(c, Rect{area.X, area.Y, colW, area.H}, page, pages, "cal_dpage", hits)
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
