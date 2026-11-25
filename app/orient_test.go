package main

import (
	"image"
	"testing"
)

// A pixel drawn at a logical point must land where a touch at that same spot reports.
func TestOrientationRoundTrip(t *testing.T) {
	for _, o := range []Orient{Portrait, LandscapeCW, LandscapeCCW} {
		lw, lh := LogicalSize(o)
		for _, p := range [][2]int{{0, 0}, {lw - 1, 0}, {0, lh - 1}, {lw - 1, lh - 1}, {500, 300}, {lw / 2, lh / 2}} {
			dx, dy := LogicalToDevice(p[0], p[1], o)
			if dx < 0 || dx >= ScreenW || dy < 0 || dy >= ScreenH {
				t.Fatalf("%v: %v maps outside the panel: %d,%d", o, p, dx, dy)
			}
			lx, ly := DeviceToLogical(dx, dy, o)
			if lx != p[0] || ly != p[1] {
				t.Errorf("%v: %v -> device (%d,%d) -> back (%d,%d)", o, p, dx, dy, lx, ly)
			}
		}
	}
}

func TestToDeviceMovesPixelsWhereTouchesExpect(t *testing.T) {
	for _, o := range []Orient{LandscapeCW, LandscapeCCW} {
		lw, lh := LogicalSize(o)
		src := image.NewGray(image.Rect(0, 0, lw, lh))
		src.Pix[300*src.Stride+500] = 77 // logical (500,300)
		dev := ToDevice(src, o)
		dx, dy := LogicalToDevice(500, 300, o)
		if got := dev.Pix[dy*dev.Stride+dx]; got != 77 {
			t.Errorf("%v: pixel for logical (500,300) not at device (%d,%d): %d", o, dx, dy, got)
		}
	}
}

func TestCornersMatchPhysicalRotation(t *testing.T) {
	// Clockwise: the top-left of the picture ends up at the top-right of the panel.
	if x, y := LogicalToDevice(0, 0, LandscapeCW); x != ScreenW-1 || y != 0 {
		t.Errorf("CW top-left -> (%d,%d)", x, y)
	}
	// Counter-clockwise: the top-left ends up at the bottom-left.
	if x, y := LogicalToDevice(0, 0, LandscapeCCW); x != 0 || y != ScreenH-1 {
		t.Errorf("CCW top-left -> (%d,%d)", x, y)
	}
}
