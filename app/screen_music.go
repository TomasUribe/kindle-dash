package main

import "fmt"

// drawMusic makes the album cover the centerpiece: as large as the screen allows, with the
// title, a hairline progress and big controls around it.
func drawMusic(c *Canvas, l layout, s *Scene, hits *[]HitZone) {
	a := l.Content
	p := s.Player
	t := playerTexts_(s)

	device := ""
	if p.Active {
		device = p.Device
		if p.Volume >= 0 {
			device += fmt.Sprintf("  ·  Volume %d%%", p.Volume)
		}
	}
	df := Face(Light, 25)

	if l.Land {
		art := min(a.H, 900)
		artR := Rect{a.X, a.Y + (a.H-art)/2, art, art}
		artBox(c, artR, s)
		col := Rect{artR.Right() + 64, a.Y, a.Right() - artR.Right() - 64, a.H}

		devR := Rect{col.X, col.Bottom() - 34, col.W, 34}
		ctlR := Rect{col.X, devR.Y - 10 - 140, col.W, 140}
		progR := Rect{col.X + 6, ctlR.Y - 14 - 66, col.W - 12, 66}
		textR := Rect{col.X, col.Y, col.W, progR.Y - 12 - col.Y}
		caption(c, stateCaption(p), Rect{col.X, textR.Y + textR.H/2 - 130, col.W, 26}, Mid, Center)
		heroText(c, textR, t, Center, 52, 36, 28, true)
		seekBar(c, Rect{progR.X, progR.Y, progR.W, 30}, s, hits)
		progressTimes(c, Rect{progR.X, progR.Y + 36, progR.W, 26}, s)
		transportRow(c, ctlR, 62, true, s, hits)
		c.TextIn(device, devR, df, Mid, Center)
		return
	}

	devR := Rect{a.X, a.Bottom() - 34, a.W, 34}
	ctlR := Rect{a.X, devR.Y - 10 - 144, a.W, 144}
	progR := Rect{a.X + 10, ctlR.Y - 12 - 66, a.W - 20, 66}
	textR := Rect{a.X, progR.Y - 12 - 168, a.W, 168}
	art := min(a.W, textR.Y-26-a.Y)
	artR := Rect{a.CX() - art/2, a.Y, art, art}
	artBox(c, artR, s)
	heroText(c, textR, t, Center, 54, 38, 28, true)
	seekBar(c, Rect{progR.X, progR.Y, progR.W, 30}, s, hits)
	progressTimes(c, Rect{progR.X, progR.Y + 36, progR.W, 26}, s)
	transportRow(c, ctlR, 66, true, s, hits)
	c.TextIn(device, devR, df, Mid, Center)
}
