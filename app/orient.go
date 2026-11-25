package main

import "image"

// The panel is always portrait (1236x1648). A landscape frame is drawn 1648x1236 and turned
// before it is sent to the screen; touches are turned back the other way.

// LogicalSize is the canvas size for an orientation.
func LogicalSize(o Orient) (w, h int) {
	if o.Landscape() {
		return ScreenH, ScreenW
	}
	return ScreenW, ScreenH
}

// ToDevice turns a logical frame into a portrait panel image.
func ToDevice(src *image.Gray, o Orient) *image.Gray {
	if o == Portrait {
		return src
	}
	lw, lh := src.Bounds().Dx(), src.Bounds().Dy() // 1648 x 1236
	dst := image.NewGray(image.Rect(0, 0, lh, lw))
	for dy := 0; dy < lw; dy++ {
		drow := dst.Pix[dy*dst.Stride : dy*dst.Stride+lh]
		for dx := 0; dx < lh; dx++ {
			var lx, ly int
			if o == LandscapeCW {
				lx, ly = dy, lh-1-dx
			} else {
				lx, ly = lw-1-dy, dx
			}
			drow[dx] = src.Pix[ly*src.Stride+lx]
		}
	}
	return dst
}

// DeviceToLogical maps a touch on the panel to a point on the logical canvas.
func DeviceToLogical(dx, dy int, o Orient) (int, int) {
	lw, lh := LogicalSize(o)
	switch o {
	case LandscapeCW:
		return dy, lh - 1 - dx
	case LandscapeCCW:
		return lw - 1 - dy, dx
	}
	return dx, dy
}

// LogicalToDevice is the inverse, used by the on-screen simulator.
func LogicalToDevice(lx, ly int, o Orient) (int, int) {
	lw, lh := LogicalSize(o)
	switch o {
	case LandscapeCW:
		return lh - 1 - ly, lx
	case LandscapeCCW:
		return ly, lw - 1 - lx
	}
	return lx, ly
}

// Next cycles portrait, landscape (USB port right), landscape (USB port left).
func (o Orient) Next() Orient { return (o + 1) % 3 }

func (o Orient) String() string {
	return [...]string{"portrait", "landscape-usb-right", "landscape-usb-left"}[o]
}
