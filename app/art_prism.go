package main

import (
	"image"
	"image/color"
	"math"
)

// prismCover is an original illustration drawn in code for the demo: a ray of light enters a glass
// triangle on black and fans out as a spectrum. It is not any real album's artwork; the demo uses it
// so the screenshots have a striking, high-contrast cover without shipping anyone's copyrighted image.
func prismCover() image.Image {
	const n = 640
	const ss = 3 // samples per axis per pixel, for smooth edges

	// the glass triangle
	A := pt{320, 190}
	B := pt{185, 430}
	C := pt{455, 430}
	// where the ray enters the left face, where it leaves the right face, and where it comes from
	in := pt{A.x + (B.x-A.x)*0.60, A.y + (B.y-A.y)*0.60}
	out := pt{A.x + (C.x-A.x)*0.66, A.y + (C.y-A.y)*0.66}
	src := pt{0, 468}
	// the fan leaves `out` towards the right edge, spread over these heights
	const fanTop, fanBottom, bands = 352.0, 548.0, 6
	a0 := math.Atan2(fanTop-out.y, n-out.x)
	a1 := math.Atan2(fanBottom-out.y, n-out.x)
	// luminance of each band, chosen to stay distinguishable in 16 grey levels
	levels := [bands]float64{235, 150, 245, 185, 110, 215}

	g := image.NewGray(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			sum := 0.0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					p := pt{float64(x) + (float64(sx)+0.5)/ss, float64(y) + (float64(sy)+0.5)/ss}
					sum += prismSample(p, A, B, C, in, out, src, a0, a1, levels[:])
				}
			}
			g.SetGray(x, y, color.Gray{Y: uint8(sum / (ss * ss))})
		}
	}
	return g
}

type pt struct{ x, y float64 }

func prismSample(p, A, B, C, in, out, src pt, a0, a1 float64, levels []float64) float64 {
	v := 0.0 // black

	if inTriangle(p, A, B, C) {
		// a faint glass body, a little lighter towards the top left
		v = 26 + 22*(1-(p.y-A.y)/(B.y-A.y))*(1-(p.x-B.x)/(C.x-B.x))
	}
	// the spectrum: everything right of the exit point between the first and last ray
	if p.x > out.x {
		ang := math.Atan2(p.y-out.y, p.x-out.x)
		if ang >= a0 && ang <= a1 {
			k := int((ang - a0) / (a1 - a0) * float64(len(levels)))
			k = max(0, min(len(levels)-1, k))
			v = levels[k]
			// thin dark seams between bands, and a fade towards the edge
			f := (ang-a0)/(a1-a0)*float64(len(levels)) - float64(k)
			if f < 0.06 || f > 0.94 {
				v *= 0.35
			}
			v *= 1 - 0.28*math.Min(1, (p.x-out.x)/(640-out.x))
		}
	}
	// the incoming ray, and its path through the glass
	if d := segDist(p, src, in); d < 3.2 {
		v = math.Max(v, 255*smooth(3.2-d))
	}
	if d := segDist(p, in, out); d < 2.4 {
		v = math.Max(v, 235*smooth(2.4-d))
	}
	// the outline of the glass
	d := math.Min(segDist(p, A, B), math.Min(segDist(p, B, C), segDist(p, C, A)))
	if d < 3.0 {
		v = math.Max(v, 250*smooth(3.0-d))
	}
	return math.Max(0, math.Min(255, v))
}

// smooth turns "how far inside a line's edge" into coverage 0..1 over one pixel.
func smooth(inside float64) float64 { return math.Max(0, math.Min(1, inside)) }

func inTriangle(p, a, b, c pt) bool {
	sign := func(p1, p2, p3 pt) float64 { return (p1.x-p3.x)*(p2.y-p3.y) - (p2.x-p3.x)*(p1.y-p3.y) }
	d1, d2, d3 := sign(p, a, b), sign(p, b, c), sign(p, c, a)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

// segDist is the distance from p to the segment a-b.
func segDist(p, a, b pt) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l2))
	}
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}
