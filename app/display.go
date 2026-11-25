package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
)

// Display shows a portrait panel image. Full does a clean flashing refresh; Partial redraws a region.
type Display interface {
	Full(dev *image.Gray) error
	Partial(dev *image.Gray, r image.Rectangle) error
}

// FBInk draws through the fbink tool that the jailbreak installs on the Kindle.
type FBInk struct {
	Bin         string
	Dir         string   // scratch folder for the PNGs handed to fbink
	FullArgs    []string // flags for a full refresh (default -c -f)
	PartialArgs []string // flags for a partial refresh (default none)
}

var fbinkCandidates = []string{
	"/mnt/us/libkh/bin/fbink",
	"/mnt/us/koreader/fbink",
	"/usr/bin/fbink",
	"/usr/local/bin/fbink",
}

func FindFBInk(explicit string) string {
	if explicit != "" {
		return explicit
	}
	for _, p := range fbinkCandidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("fbink"); err == nil {
		return p
	}
	return ""
}

func (f *FBInk) Full(dev *image.Gray) error {
	path := filepath.Join(f.Dir, "frame.png")
	if err := writePNG(path, dev); err != nil {
		return err
	}
	return f.run(append(append([]string{"-q"}, f.FullArgs...), "-g", "file="+path)...)
}

func (f *FBInk) Partial(dev *image.Gray, r image.Rectangle) error {
	path := filepath.Join(f.Dir, "part.png")
	if err := writePNG(path, dev.SubImage(r).(*image.Gray)); err != nil {
		return err
	}
	return f.run(append(append([]string{"-q"}, f.PartialArgs...), "-g", fmt.Sprintf("file=%s,x=%d,y=%d", path, r.Min.X, r.Min.Y))...)
}

func (f *FBInk) run(args ...string) error {
	out, err := exec.Command(f.Bin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("fbink %v: %v: %s", args, err, out)
	}
	return nil
}

func writePNG(path string, img *image.Gray) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NullDisplay counts updates; the simulator reads the frame straight from the app.
type NullDisplay struct{ Fulls, Partials atomic.Int64 }

func (n *NullDisplay) Full(*image.Gray) error                { n.Fulls.Add(1); return nil }
func (n *NullDisplay) Partial(*image.Gray, image.Rectangle) error { n.Partials.Add(1); return nil }

// diffRects finds the regions that differ between two frames. full is true when so much
// changed that a clean full refresh is the better choice.
func diffRects(a, b *image.Gray) (rects []image.Rectangle, full bool) {
	w, h := b.Bounds().Dx(), b.Bounds().Dy()
	type band struct{ y0, y1, x0, x1 int }
	var bands []band
	var cur *band
	gap := 0
	const mergeGap = 40
	changedPixels := 0
	for y := 0; y < h; y++ {
		ra, rb := a.Pix[y*a.Stride:y*a.Stride+w], b.Pix[y*b.Stride:y*b.Stride+w]
		x0, x1 := -1, -1
		for x := 0; x < w; x++ {
			if ra[x] != rb[x] {
				if x0 < 0 {
					x0 = x
				}
				x1 = x
				changedPixels++
			}
		}
		if x0 < 0 {
			if cur != nil {
				gap++
				if gap > mergeGap {
					bands = append(bands, *cur)
					cur = nil
				}
			}
			continue
		}
		gap = 0
		if cur == nil {
			cur = &band{y0: y, y1: y, x0: x0, x1: x1}
		} else {
			cur.y1 = y
			cur.x0, cur.x1 = min(cur.x0, x0), max(cur.x1, x1)
		}
	}
	if cur != nil {
		bands = append(bands, *cur)
	}
	if len(bands) == 0 {
		return nil, false
	}
	if changedPixels > w*h*35/100 {
		return nil, true
	}
	for _, bd := range bands {
		// pad, and snap to multiples of 8 pixels: e-ink controllers like aligned regions
		x0 := max(0, (bd.x0-8)/8*8)
		x1 := min(w, (bd.x1+8+7)/8*8)
		y0, y1 := max(0, bd.y0-6), min(h, bd.y1+7)
		rects = append(rects, image.Rect(x0, y0, x1, y1))
	}
	if len(rects) > 4 { // too fragmented: one box around everything
		u := rects[0]
		for _, r := range rects[1:] {
			u = u.Union(r)
		}
		rects = []image.Rectangle{u}
	}
	return rects, false
}
