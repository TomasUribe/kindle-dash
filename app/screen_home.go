package main

// drawHome shows what is playing on top and what is coming up below.
func drawHome(c *Canvas, l layout, s *Scene, hits *[]HitZone) {
	a := l.Content
	st := agendaStyle{title: 38, when: 28, sub: 25, hdr: 24, pad: 10}
	if l.Land {
		leftW := 610
		drawHero(c, Rect{a.X, a.Y, leftW, a.H}, true, s, hits)
		drawAgenda(c, Rect{a.X + leftW + 52, a.Y, a.W - leftW - 52, a.H}, s, st)
		return
	}
	ph := 530
	drawHero(c, Rect{a.X, a.Y, a.W, ph}, false, s, hits)
	drawAgenda(c, Rect{a.X, a.Y + ph + 36, a.W, a.H - ph - 36}, s, st)
}

// drawHero is the compact player: cover, text, play controls and a progress line.
func drawHero(c *Canvas, r Rect, vertical bool, s *Scene, hits *[]HitZone) {
	p := s.Player
	t := playerTexts_(s)

	if vertical { // landscape: cover on top, everything else underneath
		ctlH, progH, textH := 124, 64, 150
		art := min(r.W, r.H-ctlH-progH-textH-36)
		artR := Rect{r.CX() - art/2, r.Y, art, art}
		artBox(c, artR, s)
		text := Rect{r.X, artR.Bottom() + 24, r.W, textH}
		heroText(c, text, t, Center, 44, 32, 26, false)
		prog := Rect{r.X, r.Bottom() - ctlH - progH, r.W, 30}
		seekBar(c, prog, s, hits)
		progressTimes(c, Rect{prog.X, prog.Bottom() + 2, prog.W, 26}, s)
		transportRow(c, Rect{r.X, r.Bottom() - ctlH, r.W, ctlH}, 50, false, s, hits)
		return
	}

	art := min(r.H-104, 430)
	artR := Rect{r.X, r.Y, art, art}
	artBox(c, artR, s)
	col := Rect{artR.Right() + 48, r.Y, r.Right() - artR.Right() - 48, art}
	caption(c, stateCaption(p), Rect{col.X, col.Y + 6, col.W, 26}, Mid, Left)
	heroText(c, Rect{col.X, col.Y + 44, col.W, art - 44 - 130}, t, Left, 50, 34, 28, false)
	transportRow(c, Rect{col.X, artR.Bottom() - 124, col.W, 124}, 56, false, s, hits)

	prog := Rect{r.X, artR.Bottom() + 30, r.W, 30}
	seekBar(c, prog, s, hits)
	progressTimes(c, Rect{prog.X, prog.Bottom() + 2, prog.W, 26}, s)
}
