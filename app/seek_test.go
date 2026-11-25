package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"
	"time"
)

func TestSeekPermille(t *testing.T) {
	z := Rect{100, 0, 800, 30}
	for x, want := range map[int]int{100: 0, 500: 500, 900: 1000, 50: 0, 1000: 1000, 300: 250} {
		if got := seekPermille(z, x); got != want {
			t.Errorf("x=%d: %d, want %d", x, got, want)
		}
	}
}

// After a seek the line steps from where it landed, so it is drawn where the finger was.
func TestProgressIsAnchoredAtTheSeek(t *testing.T) {
	now := time.Now()
	mk := func(progress time.Duration, playing bool, fetched time.Time, anchor time.Duration) *Scene {
		return &Scene{Now: now, Cfg: &Config{ProgressSeconds: 15}, SeekAnchor: anchor,
			Player: Player{Active: true, Playing: playing, Dur: 200 * time.Second, Progress: progress, FetchedAt: fetched}}
	}
	sec := time.Second
	for _, c := range []struct {
		name string
		s    *Scene
		want time.Duration
	}{
		{"no seek: steps from the start", mk(157*sec, false, now, 0), 150 * sec},
		{"after a seek: exactly where it landed", mk(157*sec, false, now, 157*sec), 157 * sec},
		{"then in steps from there", mk(157*sec, true, now.Add(-20*sec), 157*sec), 172 * sec},
		{"the player lands a little early", mk(156200*time.Millisecond, false, now, 157*sec), 157 * sec},
		{"an old anchor far ahead is ignored", mk(30*sec, false, now, 157*sec), 30 * sec},
	} {
		if got := c.s.shownProgress(); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSpotifySeekRequest(t *testing.T) {
	f := &fakeSpotify{}
	s := newTestSpotify(t, f)
	if err := s.Seek("90500"); err != nil || len(f.seeks) != 1 || f.seeks[0] != "90500" {
		t.Fatalf("seek: %v %v", f.seeks, err)
	}
	if _, err := s.Act("seek:12000", time.Now()); err != nil || len(f.seeks) != 2 || f.seeks[1] != "12000" {
		t.Fatalf("seek through Act: %v %v", f.seeks, err)
	}
	for _, bad := range []string{"", "abc", "-5"} {
		if err := s.Seek(bad); err == nil {
			t.Errorf("Seek(%q) should fail", bad)
		}
	}
	if len(f.seeks) != 2 {
		t.Errorf("bad positions must not reach Spotify: %v", f.seeks)
	}
}

// drain runs the work the app posted to its own loop until the running command has finished.
func drain(t *testing.T, a *App) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for a.pressed != "" {
		select {
		case fn := <-a.tasks:
			fn()
		case <-deadline:
			t.Fatal("the app never answered")
		}
	}
}

func seekZone(t *testing.T, a *App) HitZone {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, z := range a.hits {
		if z.Action == seekAction {
			return z
		}
	}
	t.Fatal("no progress line on this screen")
	return HitZone{}
}

func TestTapOnProgressLineSeeks(t *testing.T) {
	for _, v := range []View{ViewHome, ViewMusic, ViewLyrics} {
		for _, o := range []Orient{Portrait, LandscapeCW} {
			a := testApp(t, t.TempDir())
			a.view, a.orient = v, o
			a.setPlayer(a.dp.State(time.Now()))
			a.draw(true)
			z := seekZone(t, a)
			dur := a.player.Dur

			// tap three quarters of the way along the line, in device coordinates
			lx, ly := z.R.X+z.R.W*3/4, z.R.CY()
			dx, dy := LogicalToDevice(lx, ly, a.orient)
			a.handle(InputEvent{Kind: "tap", X: dx, Y: dy})
			if a.pressed != "seek" {
				t.Fatalf("view %d %s: tap on the line should start a seek, pressed=%q", v, o, a.pressed)
			}
			want := dur * 3 / 4
			if d := a.player.Progress - want; d < -dur/100 || d > dur/100 {
				t.Errorf("view %d %s: progress shown %v, want about %v", v, o, a.player.Progress, want)
			}
			if !a.lyrAuto {
				t.Error("lyrics should follow the song after a seek")
			}
			drain(t, a) // the demo player applies the seek and the app takes its answer
			if a.pressed != "" {
				t.Errorf("view %d %s: still busy after the seek", v, o)
			}
			if d := a.dp.State(time.Now()).Progress - want; d < -dur/50 || d > dur/50 {
				t.Errorf("view %d %s: player is at %v, want about %v", v, o, a.dp.State(time.Now()).Progress, want)
			}
		}
	}
}

func TestDragOnProgressLineSeeks(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.view = ViewMusic
	a.setPlayer(a.dp.State(time.Now()))
	a.draw(true)
	z := seekZone(t, a)
	x0, y0 := DeviceToLogicalFor(a, z.R.X+z.R.W/10, z.R.CY())
	x1, y1 := DeviceToLogicalFor(a, z.R.X+z.R.W*9/10, z.R.CY())
	a.handle(InputEvent{Kind: "swipe", X: x0, Y: y0, DX: x1 - x0, DY: y1 - y0})
	if a.view != ViewMusic {
		t.Fatalf("a drag along the progress line must not change screens, now on view %d", a.view)
	}
	want := a.player.Dur * 9 / 10
	if d := a.player.Progress - want; d < -a.player.Dur/100 || d > a.player.Dur/100 {
		t.Errorf("progress %v, want about %v (90%%)", a.player.Progress, want)
	}
	drain(t, a)
}

// DeviceToLogicalFor converts a logical point to the device coordinates a touch would report.
func DeviceToLogicalFor(a *App, lx, ly int) (int, int) { return LogicalToDevice(lx, ly, a.orient) }

func TestSeekNeedsSomethingPlaying(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.view = ViewMusic
	a.player = Player{Volume: -1}
	a.seekTo(500)
	if a.pressed != "" || a.status == "" {
		t.Errorf("nothing playing: no command, a short message instead (pressed=%q status=%q)", a.pressed, a.status)
	}
	// the very end must not skip to the next song
	a.setPlayer(a.dp.State(time.Now()))
	a.seekTo(1000)
	if left := a.player.Dur - a.player.Progress; left < time.Second {
		t.Errorf("seeking to the end should stop short of it, %v left", left)
	}
	drain(t, a)
}

// KDASH_DEMO_COVER lets anyone make screenshots with their own picture instead of the drawn prism.
func TestDemoCoverCanBeOverridden(t *testing.T) {
	img := prismCoverOrOverride(t, "")
	if img.Bounds().Dx() != 640 {
		t.Fatalf("default demo cover is %v", img.Bounds())
	}
	f := t.TempDir() + "/c.png"
	small := gray(10, 10, 200)
	var buf bytes.Buffer
	if err := png.Encode(&buf, small); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := prismCoverOrOverride(t, f); got.Bounds().Dx() != 10 {
		t.Errorf("override ignored: %v", got.Bounds())
	}
	if got := prismCoverOrOverride(t, "/no/such/file.png"); got.Bounds().Dx() != 640 {
		t.Errorf("a missing override file should fall back to the drawn cover: %v", got.Bounds())
	}
}

func prismCoverOrOverride(t *testing.T, env string) image.Image {
	t.Helper()
	t.Setenv("KDASH_DEMO_COVER", env)
	img, err := loadArt("demo:prism")
	if err != nil {
		t.Fatal(err)
	}
	return img
}
