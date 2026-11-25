package main

import (
	"encoding/binary"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ev encodes one Linux input_event the way this machine's kernel would.
func ev(typ, code uint16, val int32) []byte {
	b := make([]byte, evSize)
	binary.LittleEndian.PutUint16(b[tvSize:], typ)
	binary.LittleEndian.PutUint16(b[tvSize+2:], code)
	binary.LittleEndian.PutUint32(b[tvSize+4:], uint32(val))
	return b
}

func stream(events ...[]byte) []byte {
	var out []byte
	for _, e := range events {
		out = append(out, e...)
	}
	return out
}

func watch(t *testing.T, data []byte) []InputEvent {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event0")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out := make(chan InputEvent, 8)
	if err := WatchTouch(path, out, t.Logf); err != nil {
		t.Fatal(err)
	}
	var got []InputEvent
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-out:
			got = append(got, e)
		case <-time.After(200 * time.Millisecond):
			return got
		case <-deadline:
			return got
		}
	}
}

func TestTouchTapWithButtonEvents(t *testing.T) {
	got := watch(t, stream(
		ev(evAbs, absMTX, 600), ev(evAbs, absMTY, 900), ev(evKey, btnTouch, 1), ev(evSyn, 0, 0),
		ev(evKey, btnTouch, 0), ev(evSyn, 0, 0),
	))
	if len(got) != 1 || got[0].Kind != "tap" || got[0].X != 600 || got[0].Y != 900 {
		t.Fatalf("got %+v", got)
	}
}

func TestTouchTapWithTrackingID(t *testing.T) {
	got := watch(t, stream(
		ev(evAbs, absMTID, 5), ev(evAbs, absMTX, 100), ev(evAbs, absMTY, 200), ev(evSyn, 0, 0),
		ev(evAbs, absMTX, 103), ev(evAbs, absMTY, 204), ev(evSyn, 0, 0), // a little jitter
		ev(evAbs, absMTID, -1), ev(evSyn, 0, 0),
	))
	if len(got) != 1 || got[0].Kind != "tap" || got[0].X != 103 || got[0].Y != 204 {
		t.Fatalf("got %+v", got)
	}
}

func TestTouchSwipe(t *testing.T) {
	got := watch(t, stream(
		ev(evAbs, absMTX, 900), ev(evAbs, absMTY, 700), ev(evKey, btnTouch, 1), ev(evSyn, 0, 0),
		ev(evAbs, absMTX, 500), ev(evAbs, absMTY, 705), ev(evSyn, 0, 0),
		ev(evAbs, absMTX, 300), ev(evAbs, absMTY, 710), ev(evSyn, 0, 0),
		ev(evKey, btnTouch, 0), ev(evSyn, 0, 0),
	))
	if len(got) != 1 || got[0].Kind != "swipe" || got[0].DX != -600 || abs(got[0].DY) > 20 {
		t.Fatalf("got %+v", got)
	}
}

func TestTwoSeparateTaps(t *testing.T) {
	got := watch(t, stream(
		ev(evAbs, absMTX, 10), ev(evAbs, absMTY, 20), ev(evKey, btnTouch, 1), ev(evSyn, 0, 0), ev(evKey, btnTouch, 0), ev(evSyn, 0, 0),
		ev(evAbs, absMTX, 800), ev(evAbs, absMTY, 1500), ev(evKey, btnTouch, 1), ev(evSyn, 0, 0), ev(evKey, btnTouch, 0), ev(evSyn, 0, 0),
	))
	if len(got) != 2 || got[0].X != 10 || got[1].X != 800 || got[1].Y != 1500 {
		t.Fatalf("got %+v", got)
	}
}

func gray(w, h int, fill uint8) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for i := range g.Pix {
		g.Pix[i] = fill
	}
	return g
}

func TestDiffRects(t *testing.T) {
	a, b := gray(1236, 1648, 255), gray(1236, 1648, 255)
	if r, full := diffRects(a, b); len(r) != 0 || full {
		t.Fatalf("identical frames: %v %v", r, full)
	}
	// a small change: only a small region should be redrawn, aligned to 8 pixels
	for y := 200; y < 230; y++ {
		for x := 301; x < 380; x++ {
			b.Pix[y*b.Stride+x] = 0
		}
	}
	r, full := diffRects(a, b)
	if full || len(r) != 1 {
		t.Fatalf("small change: %v %v", r, full)
	}
	if r[0].Min.X%8 != 0 || r[0].Max.X%8 != 0 || !r[0].In(b.Bounds()) {
		t.Errorf("not aligned/clipped: %v", r[0])
	}
	if !(image.Rect(301, 200, 380, 230)).In(r[0]) {
		t.Errorf("region %v does not cover the change", r[0])
	}
	// two distant changes become two regions
	for y := 1400; y < 1420; y++ {
		b.Pix[y*b.Stride+900] = 0
	}
	if r, _ := diffRects(a, b); len(r) != 2 {
		t.Errorf("two distant changes: %v", r)
	}
	// most of the screen changing means a clean full refresh
	if _, full := diffRects(a, gray(1236, 1648, 0)); !full {
		t.Error("whole-screen change should ask for a full refresh")
	}
}
