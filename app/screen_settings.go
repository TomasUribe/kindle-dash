package main

import "fmt"

func IconMinus(c *Canvas, cx, cy, s float64, v uint8) {
	c.Shape(Rect{int(cx - s*0.5), int(cy - s*0.15), int(s), int(s * 0.3)}, v, func(p *Path) {
		roundRectPath(p, cx-s*0.4, cy-s*0.07, s*0.8, s*0.14, s*0.04)
	})
}

func IconPlus(c *Canvas, cx, cy, s float64, v uint8) {
	c.Shape(Rect{int(cx - s*0.5), int(cy - s*0.5), int(s), int(s)}, v, func(p *Path) {
		roundRectPath(p, cx-s*0.4, cy-s*0.07, s*0.8, s*0.14, s*0.04)
		roundRectPath(p, cx-s*0.07, cy-s*0.4, s*0.14, s*0.8, s*0.04)
	})
}

// drawSettings is the quick-settings screen: brightness, warmth (when the Kindle has it), and a few switches.
func drawSettings(c *Canvas, l layout, s *Scene, hits *[]HitZone) {
	a, ls := l.Content, s.Light
	lights := Rect{a.X, a.Y, a.W, a.H}
	switches := lights
	if l.Land {
		lw := (a.W - 64) * 11 / 20
		lights = Rect{a.X, a.Y, lw, a.H}
		switches = Rect{a.X + lw + 64, a.Y, a.W - lw - 64, a.H}
	}

	y := lights.Y
	y = lightSection(c, lights.X, y, lights.W, "Brightness", true, ls.Available, ls.Level, ls.Max, "fl", true, s, hits)
	if ls.HasWarmth {
		y = lightSection(c, lights.X, y, lights.W, "Warmth", false, true, ls.Warmth, ls.WarmMax, "warm", false, s, hits)
	}
	if l.Land {
		y = switches.Y
	}
	drawSwitches(c, Rect{switches.X, y, switches.W, switches.Bottom() - y}, s, hits)
}

// lightSection is a title, a slider with - and + buttons, and (for brightness) presets.
// It returns the y just below itself.
func lightSection(c *Canvas, x, y, w int, title string, sun, enabled bool, level, maxLvl int, key string, presets bool, s *Scene, hits *[]HitZone) int {
	tf, vf := Face(Bold, 38), Face(Medium, 34)
	tx := x
	if sun {
		IconSun(c, float64(x+22), float64(y+LineHeight(tf)/2+3), 44, Black)
		tx += 60
	}
	c.TextIn(title, Rect{tx, y, w / 2, LineHeight(tf)}, tf, Black, Left)
	value := fmt.Sprintf("%d / %d", level, maxLvl)
	switch {
	case !enabled:
		value = "Not available"
	case level == 0:
		value = "Off"
	}
	c.TextIn(value, Rect{x + w/2, y + 2, w / 2, LineHeight(tf)}, vf, Dark, Right)
	y += LineHeight(tf) + 14

	rowH := 96
	roundButton(c, x+44, y+rowH/2, 40, key+"_down", IconMinus, 40, btnOutlined, enabled && level > 0, s, hits)
	roundButton(c, x+w-44, y+rowH/2, 40, key+"_up", IconPlus, 40, btnOutlined, enabled && level < maxLvl, s, hits)
	slider(c, Rect{x + 100, y, w - 200, rowH}, level, maxLvl, key, enabled, hits)
	y += rowH + 16

	if presets {
		vals := []int{0, 4, 10, 17, maxLvl}
		sel := -1
		for i, v := range vals {
			if enabled && level == v {
				sel = i
			}
		}
		segmented(c, Rect{x, y, w, 72}, []string{"Off", "Low", "Medium", "High", "Max"}, sel, enabled,
			func(i int) string { return fmt.Sprintf("%s:%d", key, vals[i]) }, hits)
		y += 72 + 16
	}
	hairline(c, x, y+6, w, Pale)
	return y + 36
}

func drawSwitches(c *Canvas, r Rect, s *Scene, hits *[]HitZone) {
	lf, df := Face(Bold, 38), Face(Regular, 27)
	rowH := min(134, r.H/4)
	row := func(i int, label, desc string, control func(box Rect, cy int)) {
		box := Rect{r.X, r.Y + i*rowH, r.W, rowH}
		c.TextIn(label, Rect{box.X, box.CY() - LineHeight(lf) + 4, box.W - 260, LineHeight(lf)}, lf, Black, Left)
		c.TextIn(desc, Rect{box.X, box.CY() + 2, box.W - 260, LineHeight(df)}, df, Mid, Left)
		control(box, box.CY())
		if i < 3 {
			hairline(c, box.X, box.Bottom()-2, box.W, Pale)
		}
	}
	row(0, "Night mode", "White on black, easier in the dark", func(box Rect, cy int) {
		toggleSwitch(c, Rect{box.Right() - 124, cy - 34, 124, 68}, s.Night, "night", hits)
	})
	row(1, "Clock", "Show 12 or 24 hour time", func(box Rect, cy int) {
		sel := 0
		if s.Cfg.Use24h() {
			sel = 1
		}
		segmented(c, Rect{box.Right() - 230, cy - 33, 230, 66}, []string{"12h", "24h"}, sel, true, func(int) string { return "clock" }, hits)
	})
	row(2, "Refresh screen", "Clears ghosting with a full flash", func(box Rect, cy int) {
		pillButton(c, Rect{box.Right() - 200, cy - 33, 200, 66}, "Refresh", "full", false, true, hits)
	})
	row(3, "Sync now", "Reload calendars and music", func(box Rect, cy int) {
		pillButton(c, Rect{box.Right() - 200, cy - 33, 200, 66}, "Sync", "sync", false, true, hits)
	})
}
