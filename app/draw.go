package main

import (
	"embed"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

//go:embed fonts/*.ttf
var fontFS embed.FS

// Gray levels. The Paperwhite shows 16 shades, so keep a handful that stay distinct.
const (
	Black uint8 = 0
	Dark  uint8 = 70
	Mid   uint8 = 135
	Soft  uint8 = 190
	Pale  uint8 = 232
	White uint8 = 255
)

type Weight int

const (
	Light Weight = iota
	Regular
	Medium
	Bold
)

var fontFiles = map[Weight]string{Light: "Ubuntu-L.ttf", Regular: "Ubuntu-R.ttf", Medium: "Ubuntu-M.ttf", Bold: "Ubuntu-B.ttf"}

var (
	fontsMu sync.Mutex
	parsed  = map[Weight]*opentype.Font{}
	faces   = map[faceKey]font.Face{}
)

type faceKey struct {
	w    Weight
	size int
}

// Face returns a cached font face at a pixel size.
func Face(w Weight, size int) font.Face {
	fontsMu.Lock()
	defer fontsMu.Unlock()
	k := faceKey{w, size}
	if f, ok := faces[k]; ok {
		return f
	}
	ft, ok := parsed[w]
	if !ok {
		raw, err := fontFS.ReadFile("fonts/" + fontFiles[w])
		if err != nil {
			panic(err)
		}
		if ft, err = opentype.Parse(raw); err != nil {
			panic(err)
		}
		parsed[w] = ft
	}
	f, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	faces[k] = f
	return f
}

// Rect is an integer rectangle, X/Y top-left.
type Rect struct{ X, Y, W, H int }

func (r Rect) Right() int  { return r.X + r.W }
func (r Rect) Bottom() int { return r.Y + r.H }
func (r Rect) CX() int     { return r.X + r.W/2 }
func (r Rect) CY() int     { return r.Y + r.H/2 }
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.Right() && y >= r.Y && y < r.Bottom()
}
func (r Rect) Inset(d int) Rect { return Rect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d} }
func (r Rect) Image() image.Rectangle {
	return image.Rect(r.X, r.Y, r.Right(), r.Bottom())
}

// Canvas is a grayscale drawing surface.
type Canvas struct {
	Img  *image.Gray
	W, H int
}

func NewCanvas(w, h int) *Canvas {
	c := &Canvas{Img: image.NewGray(image.Rect(0, 0, w, h)), W: w, H: h}
	c.Fill(Rect{0, 0, w, h}, White)
	return c
}

func (c *Canvas) Fill(r Rect, v uint8) {
	draw.Draw(c.Img, r.Image().Intersect(c.Img.Bounds()), image.NewUniform(color.Gray{v}), image.Point{}, draw.Src)
}

// Path collects vector segments in canvas coordinates.
type Path struct {
	r      *vector.Rasterizer
	ox, oy float32
}

func (p *Path) Move(x, y float64)    { p.r.MoveTo(float32(x)-p.ox, float32(y)-p.oy) }
func (p *Path) Line(x, y float64)    { p.r.LineTo(float32(x)-p.ox, float32(y)-p.oy) }
func (p *Path) Close()               { p.r.ClosePath() }
func (p *Path) Cubic(a, b, c, d, e, f float64) {
	p.r.CubeTo(float32(a)-p.ox, float32(b)-p.oy, float32(c)-p.ox, float32(d)-p.oy, float32(e)-p.ox, float32(f)-p.oy)
}

// Shape fills an anti-aliased path. bounds must contain the whole shape.
func (c *Canvas) Shape(bounds Rect, v uint8, build func(p *Path)) {
	b := bounds.Image().Intersect(c.Img.Bounds())
	if b.Empty() {
		return
	}
	rz := vector.NewRasterizer(b.Dx(), b.Dy())
	build(&Path{r: rz, ox: float32(b.Min.X), oy: float32(b.Min.Y)})
	rz.Draw(c.Img, b, image.NewUniform(color.Gray{v}), image.Point{})
}

func roundRectPath(p *Path, x, y, w, h, r float64) {
	r = math.Min(r, math.Min(w, h)/2)
	k := 0.5522847498 * r
	p.Move(x+r, y)
	p.Line(x+w-r, y)
	p.Cubic(x+w-r+k, y, x+w, y+r-k, x+w, y+r)
	p.Line(x+w, y+h-r)
	p.Cubic(x+w, y+h-r+k, x+w-r+k, y+h, x+w-r, y+h)
	p.Line(x+r, y+h)
	p.Cubic(x+r-k, y+h, x, y+h-r+k, x, y+h-r)
	p.Line(x, y+r)
	p.Cubic(x, y+r-k, x+r-k, y, x+r, y)
	p.Close()
}

func (c *Canvas) RoundRect(r Rect, rad int, v uint8) {
	c.Shape(r, v, func(p *Path) { roundRectPath(p, float64(r.X), float64(r.Y), float64(r.W), float64(r.H), float64(rad)) })
}

// Card draws a filled rounded rectangle with a border.
func (c *Canvas) Card(r Rect, rad, border int, fill, edge uint8) {
	c.RoundRect(r, rad, edge)
	if border > 0 {
		c.RoundRect(r.Inset(border), max(rad-border, 0), fill)
	}
}

func (c *Canvas) Circle(cx, cy, rad float64, v uint8) {
	b := Rect{int(cx - rad - 1), int(cy - rad - 1), int(2*rad + 3), int(2*rad + 3)}
	k := 0.5522847498 * rad
	c.Shape(b, v, func(p *Path) {
		p.Move(cx+rad, cy)
		p.Cubic(cx+rad, cy+k, cx+k, cy+rad, cx, cy+rad)
		p.Cubic(cx-k, cy+rad, cx-rad, cy+k, cx-rad, cy)
		p.Cubic(cx-rad, cy-k, cx-k, cy-rad, cx, cy-rad)
		p.Cubic(cx+k, cy-rad, cx+rad, cy-k, cx+rad, cy)
		p.Close()
	})
}

func (c *Canvas) Poly(v uint8, pts ...float64) {
	minX, minY, maxX, maxY := math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64
	for i := 0; i < len(pts); i += 2 {
		minX, maxX = math.Min(minX, pts[i]), math.Max(maxX, pts[i])
		minY, maxY = math.Min(minY, pts[i+1]), math.Max(maxY, pts[i+1])
	}
	b := Rect{int(minX) - 2, int(minY) - 2, int(maxX-minX) + 5, int(maxY-minY) + 5}
	c.Shape(b, v, func(p *Path) {
		p.Move(pts[0], pts[1])
		for i := 2; i < len(pts); i += 2 {
			p.Line(pts[i], pts[i+1])
		}
		p.Close()
	})
}

// Arc draws a thick circular arc (angles in degrees, 0 = right, clockwise on screen).
func (c *Canvas) Arc(cx, cy, rad, thick, from, to float64, v uint8) {
	b := Rect{int(cx - rad - thick), int(cy - rad - thick), int(2*(rad+thick)) + 2, int(2*(rad+thick)) + 2}
	c.Shape(b, v, func(p *Path) {
		steps := int(math.Abs(to-from)/4) + 2
		ro, ri := rad+thick/2, rad-thick/2
		for i := 0; i <= steps; i++ {
			a := (from + (to-from)*float64(i)/float64(steps)) * math.Pi / 180
			x, y := cx+ro*math.Cos(a), cy+ro*math.Sin(a)
			if i == 0 {
				p.Move(x, y)
			} else {
				p.Line(x, y)
			}
		}
		for i := steps; i >= 0; i-- {
			a := (from + (to-from)*float64(i)/float64(steps)) * math.Pi / 180
			p.Line(cx+ri*math.Cos(a), cy+ri*math.Sin(a))
		}
		p.Close()
	})
}

// --- text -------------------------------------------------------------------

func TextWidth(f font.Face, s string) int {
	return font.MeasureString(f, s).Ceil()
}

func (c *Canvas) Text(s string, x, baseline int, f font.Face, v uint8) {
	d := font.Drawer{Dst: c.Img, Src: image.NewUniform(color.Gray{v}), Face: f, Dot: fixed.P(x, baseline)}
	d.DrawString(s)
}

type Align int

const (
	Left Align = iota
	Center
	Right
)

// TextIn writes one line inside a box, trimmed with an ellipsis. y is the top of the line box.
func (c *Canvas) TextIn(s string, box Rect, f font.Face, v uint8, a Align) {
	s = Ellipsize(f, s, box.W)
	w := TextWidth(f, s)
	x := box.X
	switch a {
	case Center:
		x = box.X + (box.W-w)/2
	case Right:
		x = box.Right() - w
	}
	m := f.Metrics()
	c.Text(s, x, box.Y+m.Ascent.Ceil(), f, v)
}

func LineHeight(f font.Face) int { return f.Metrics().Height.Ceil() }
func Ascent(f font.Face) int     { return f.Metrics().Ascent.Ceil() }

// Ellipsize shortens s with "…" until it fits.
func Ellipsize(f font.Face, s string, maxW int) string {
	if maxW <= 0 || TextWidth(f, s) <= maxW {
		return s
	}
	rs := []rune(s)
	for len(rs) > 0 {
		rs = rs[:len(rs)-1]
		t := strings.TrimRight(string(rs), " ,.;:-") + "…"
		if TextWidth(f, t) <= maxW {
			return t
		}
	}
	return "…"
}

// Wrap breaks s into at most maxLines lines; the last line is ellipsized if the text is longer.
func Wrap(f font.Face, s string, maxW, maxLines int) []string {
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for i := 0; i < len(words); i++ {
		w := words[i]
		trial := w
		if cur != "" {
			trial = cur + " " + w
		}
		if TextWidth(f, trial) <= maxW {
			cur = trial
			continue
		}
		if cur == "" { // a single word wider than the line
			lines = append(lines, Ellipsize(f, w, maxW))
			continue
		}
		lines = append(lines, cur)
		cur = w
		if len(lines) == maxLines-1 {
			cur = strings.Join(words[i:], " ")
			break
		}
	}
	if cur != "" {
		lines = append(lines, Ellipsize(f, cur, maxW))
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines
}

// --- images -----------------------------------------------------------------

// ToGray converts any image to grayscale and stretches its contrast a little, which suits e-ink.
func ToGray(src image.Image, w, h int) *image.Gray {
	b := src.Bounds()
	g := image.NewGray(image.Rect(0, 0, w, h))
	// Crop to a centered square-ish region matching the target aspect, then scale.
	sw, sh := b.Dx(), b.Dy()
	want := float64(w) / float64(h)
	crop := b
	if float64(sw)/float64(sh) > want {
		nw := int(float64(sh) * want)
		crop = image.Rect(b.Min.X+(sw-nw)/2, b.Min.Y, b.Min.X+(sw-nw)/2+nw, b.Max.Y)
	} else if float64(sw)/float64(sh) < want {
		nh := int(float64(sw) / want)
		crop = image.Rect(b.Min.X, b.Min.Y+(sh-nh)/2, b.Max.X, b.Min.Y+(sh-nh)/2+nh)
	}
	xdraw.CatmullRom.Scale(g, g.Bounds(), src, crop, xdraw.Src, nil)
	return stretch(g, 0.02, 0.98)
}

// stretch maps the lo/hi percentile gray levels to black/white.
func stretch(g *image.Gray, lo, hi float64) *image.Gray {
	var hist [256]int
	for _, v := range g.Pix {
		hist[v]++
	}
	total := len(g.Pix)
	var acc int
	a, b := 0, 255
	for i := 0; i < 256; i++ {
		acc += hist[i]
		if float64(acc) >= lo*float64(total) {
			a = i
			break
		}
	}
	acc = 0
	for i := 255; i >= 0; i-- {
		acc += hist[i]
		if float64(acc) >= (1-hi)*float64(total) {
			b = i
			break
		}
	}
	if b-a < 30 {
		return g
	}
	for i, v := range g.Pix {
		n := (int(v) - a) * 255 / (b - a)
		g.Pix[i] = uint8(min(255, max(0, n)))
	}
	return g
}

// DrawImage places a gray image with rounded corners.
func (c *Canvas) DrawImage(img *image.Gray, r Rect, rad int) {
	mask := image.NewAlpha(image.Rect(0, 0, r.W, r.H))
	rz := vector.NewRasterizer(r.W, r.H)
	roundRectPath(&Path{r: rz}, 0, 0, float64(r.W), float64(r.H), float64(rad))
	rz.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	draw.DrawMask(c.Img, r.Image(), img, img.Bounds().Min, mask, image.Point{}, draw.Over)
}

// --- icons ------------------------------------------------------------------
// Every icon is centered on (cx, cy) and fits a square of side s.

func IconPlay(c *Canvas, cx, cy, s float64, v uint8) {
	c.Poly(v, cx-s*0.30, cy-s*0.44, cx-s*0.30, cy+s*0.44, cx+s*0.44, cy)
}

func IconPause(c *Canvas, cx, cy, s float64, v uint8) {
	w, h := s*0.26, s*0.88
	for _, dx := range []float64{-0.27, 0.27} {
		x := cx + dx*s - w/2
		c.Shape(Rect{int(x) - 2, int(cy-h/2) - 2, int(w) + 5, int(h) + 5}, v, func(p *Path) {
			roundRectPath(p, x, cy-h/2, w, h, s*0.05)
		})
	}
}

func IconNext(c *Canvas, cx, cy, s float64, v uint8) {
	c.Poly(v, cx-s*0.42, cy-s*0.40, cx-s*0.42, cy+s*0.40, cx+s*0.22, cy)
	c.Shape(Rect{int(cx + s*0.2), int(cy - s*0.45), int(s * 0.3), int(s * 0.9)}, v, func(p *Path) {
		roundRectPath(p, cx+s*0.26, cy-s*0.42, s*0.17, s*0.84, s*0.04)
	})
}

func IconPrev(c *Canvas, cx, cy, s float64, v uint8) {
	c.Poly(v, cx+s*0.42, cy-s*0.40, cx+s*0.42, cy+s*0.40, cx-s*0.22, cy)
	c.Shape(Rect{int(cx - s*0.55), int(cy - s*0.45), int(s * 0.4), int(s * 0.9)}, v, func(p *Path) {
		roundRectPath(p, cx-s*0.43, cy-s*0.42, s*0.17, s*0.84, s*0.04)
	})
}

func iconSpeaker(c *Canvas, cx, cy, s float64, v uint8) {
	c.Poly(v, cx-s*0.44, cy-s*0.16, cx-s*0.26, cy-s*0.16, cx-s*0.02, cy-s*0.38, cx-s*0.02, cy+s*0.38,
		cx-s*0.26, cy+s*0.16, cx-s*0.44, cy+s*0.16)
}

func IconVolDown(c *Canvas, cx, cy, s float64, v uint8) {
	iconSpeaker(c, cx-s*0.08, cy, s, v)
	c.Shape(Rect{int(cx + s*0.12), int(cy - s*0.1), int(s * 0.4), int(s * 0.2)}, v, func(p *Path) {
		roundRectPath(p, cx+s*0.16, cy-s*0.055, s*0.32, s*0.11, s*0.03)
	})
}

func IconVolUp(c *Canvas, cx, cy, s float64, v uint8) {
	iconSpeaker(c, cx-s*0.12, cy, s, v)
	c.Shape(Rect{int(cx + s*0.06), int(cy - s*0.25), int(s * 0.5), int(s * 0.5)}, v, func(p *Path) {
		roundRectPath(p, cx+s*0.11, cy-s*0.055, s*0.34, s*0.11, s*0.03)
		roundRectPath(p, cx+s*0.225, cy-s*0.17, s*0.11, s*0.34, s*0.03)
	})
}

func IconHome(c *Canvas, cx, cy, s float64, v uint8, bg uint8) {
	c.Poly(v, cx, cy-s*0.46, cx+s*0.5, cy-s*0.02, cx-s*0.5, cy-s*0.02)
	c.Shape(Rect{int(cx - s*0.4), int(cy - s*0.1), int(s * 0.8), int(s * 0.6)}, v, func(p *Path) {
		roundRectPath(p, cx-s*0.34, cy-s*0.06, s*0.68, s*0.52, s*0.04)
	})
	c.Shape(Rect{int(cx - s*0.15), int(cy + s*0.1), int(s * 0.3), int(s * 0.4)}, bg, func(p *Path) {
		roundRectPath(p, cx-s*0.11, cy+s*0.14, s*0.22, s*0.32, s*0.03)
	})
}

func IconCalendar(c *Canvas, cx, cy, s float64, v uint8, bg uint8) {
	x, y, w, h := cx-s*0.45, cy-s*0.4, s*0.9, s*0.84
	c.Shape(Rect{int(x) - 2, int(y) - 2, int(w) + 5, int(h) + 5}, v, func(p *Path) { roundRectPath(p, x, y, w, h, s*0.1) })
	c.Shape(Rect{int(x) + 2, int(y+h*0.28), int(w) - 3, int(h*0.64)}, bg, func(p *Path) {
		roundRectPath(p, x+s*0.07, y+h*0.3, w-s*0.14, h*0.62, s*0.04)
	})
	for _, dx := range []float64{-0.24, 0.24} {
		c.Shape(Rect{int(cx + dx*s - s*0.07), int(cy - s*0.5), int(s * 0.14), int(s * 0.28)}, v, func(p *Path) {
			roundRectPath(p, cx+dx*s-s*0.05, cy-s*0.48, s*0.1, s*0.24, s*0.04)
		})
	}
	for r := 0; r < 2; r++ {
		for k := 0; k < 3; k++ {
			bx, by := cx-s*0.31+float64(k)*s*0.25, cy+float64(r)*s*0.25-s*0.04
			c.Shape(Rect{int(bx) - 2, int(by) - 2, int(s*0.16) + 5, int(s*0.16) + 5}, v, func(p *Path) {
				roundRectPath(p, bx, by, s*0.16, s*0.16, s*0.03)
			})
		}
	}
}

func IconMusic(c *Canvas, cx, cy, s float64, v uint8) {
	c.Circle(cx-s*0.2, cy+s*0.3, s*0.19, v)
	c.Shape(Rect{int(cx - s*0.05), int(cy - s*0.45), int(s * 0.25), int(s * 0.8)}, v, func(p *Path) {
		roundRectPath(p, cx-s*0.03, cy-s*0.42, s*0.12, s*0.74, s*0.02)
	})
	c.Poly(v, cx+s*0.07, cy-s*0.44, cx+s*0.42, cy-s*0.22, cx+s*0.42, cy-s*0.02, cx+s*0.07, cy-s*0.2)
}

func IconRotate(c *Canvas, cx, cy, s float64, v uint8) {
	c.Arc(cx, cy, s*0.32, s*0.13, 40, 330, v)
	// arrow head at the end of the arc (330 degrees)
	a := 330.0 * math.Pi / 180
	tx, ty := cx+s*0.32*math.Cos(a), cy+s*0.32*math.Sin(a)
	c.Poly(v, tx-s*0.19, ty-s*0.06, tx+s*0.2, ty-s*0.1, tx+s*0.05, ty+s*0.2)
}

func IconPower(c *Canvas, cx, cy, s float64, v uint8) {
	c.Arc(cx, cy+s*0.04, s*0.34, s*0.12, -65, 245, v)
	c.Shape(Rect{int(cx - s*0.08), int(cy - s*0.5), int(s * 0.16), int(s * 0.5)}, v, func(p *Path) {
		roundRectPath(p, cx-s*0.06, cy-s*0.48, s*0.12, s*0.46, s*0.06)
	})
}

func IconBattery(c *Canvas, x, y, w, h int, pct int, v uint8) {
	r := Rect{x, y, w, h}
	c.Card(r, 5, 3, White, v)
	c.Fill(Rect{x + w, y + h/4, 5, h / 2}, v)
	fill := (w - 10) * min(100, max(0, pct)) / 100
	if fill > 0 {
		c.Fill(Rect{x + 5, y + 5, fill, h - 10}, v)
	}
}

func IconGear(c *Canvas, cx, cy, s float64, v, bg uint8) {
	c.Circle(cx, cy, s*0.30, v)
	for k := 0; k < 8; k++ {
		a := float64(k) * math.Pi / 4
		dx, dy := math.Cos(a), math.Sin(a)
		px, py := -dy, dx // perpendicular
		r1, r2, w1, w2 := s*0.26, s*0.47, s*0.12, s*0.085
		c.Poly(v,
			cx+dx*r1+px*w1, cy+dy*r1+py*w1,
			cx+dx*r2+px*w2, cy+dy*r2+py*w2,
			cx+dx*r2-px*w2, cy+dy*r2-py*w2,
			cx+dx*r1-px*w1, cy+dy*r1-py*w1)
	}
	c.Circle(cx, cy, s*0.13, bg)
}

// IconSun is a disc with eight rays.
func IconSun(c *Canvas, cx, cy, s float64, v uint8) {
	c.Circle(cx, cy, s*0.2, v)
	for k := 0; k < 8; k++ {
		a := float64(k) * math.Pi / 4
		dx, dy := math.Cos(a), math.Sin(a)
		px, py := -dy, dx
		r1, r2, w := s*0.31, s*0.47, s*0.045
		c.Poly(v,
			cx+dx*r1+px*w, cy+dy*r1+py*w, cx+dx*r2+px*w, cy+dy*r2+py*w,
			cx+dx*r2-px*w, cy+dy*r2-py*w, cx+dx*r1-px*w, cy+dy*r1-py*w)
	}
}

// IconLibrary is a list: three rows, each with a bullet.
func IconLibrary(c *Canvas, cx, cy, s float64, v, bg uint8) {
	for k := -1; k <= 1; k++ {
		y := cy + float64(k)*s*0.3
		c.Circle(cx-s*0.36, y, s*0.07, v)
		c.Shape(Rect{int(cx - s*0.2), int(y - s*0.08), int(s * 0.7), int(s * 0.16)}, v, func(p *Path) {
			roundRectPath(p, cx-s*0.2, y-s*0.065, s*0.62, s*0.13, s*0.04)
		})
	}
}

// IconLyrics is a speech bubble with three lines of text.
func IconLyrics(c *Canvas, cx, cy, s float64, v, bg uint8) {
	x, y, w, h := cx-s*0.46, cy-s*0.4, s*0.92, s*0.66
	c.Shape(Rect{int(x) - 2, int(y) - 2, int(w) + 5, int(h) + s5(s)}, v, func(p *Path) { roundRectPath(p, x, y, w, h, s*0.12) })
	c.Poly(v, cx-s*0.22, y+h-2, cx-s*0.3, cy+s*0.46, cx+s*0.06, y+h-2)
	for k := 0; k < 3; k++ {
		ly := y + s*0.14 + float64(k)*s*0.17
		lw := w - s*0.3
		if k == 2 {
			lw *= 0.6
		}
		c.Shape(Rect{int(x + s*0.12), int(ly), int(lw) + 2, int(s * 0.1)}, bg, func(p *Path) {
			roundRectPath(p, x+s*0.15, ly, lw, s*0.085, s*0.03)
		})
	}
}

func s5(s float64) int { return int(s*0.5) + 5 }

// sortedKeys is a tiny helper for deterministic output in tests.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var _ = utf8.RuneLen
