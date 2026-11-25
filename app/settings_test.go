package main

import (
	"path/filepath"
	"testing"
)

func testApp(t *testing.T, stateDir string) *App {
	t.Helper()
	cfg := DemoConfig()
	cfg.StateFile = filepath.Join(stateDir, "state.json")
	app := NewApp(cfg, true, &NullDisplay{}, make(chan InputEvent), t.Logf)
	app.lightSt = app.light.Read()
	return app
}

func TestLightActions(t *testing.T) {
	a := testApp(t, t.TempDir())
	if !a.lightSt.Available || a.lightSt.Level != 12 {
		t.Fatalf("simulated light should start at 12: %+v", a.lightSt)
	}
	steps := []struct {
		act  string
		want int
	}{{"fl_up", 13}, {"fl_down", 12}, {"fl:24", 24}, {"fl_up", 24}, {"fl:0", 0}, {"fl_down", 0}, {"fl:7", 7}}
	for _, s := range steps {
		a.settingsAction(s.act)
		if a.lightSt.Level != s.want {
			t.Errorf("%s: level %d, want %d", s.act, a.lightSt.Level, s.want)
		}
	}
	a.settingsAction("warm_up")
	a.settingsAction("warm:20")
	if a.lightSt.Warmth != 20 {
		t.Errorf("warmth %d, want 20", a.lightSt.Warmth)
	}
}

func TestNightAndClockAreRemembered(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	wasH24 := a.cfg.Use24h()
	a.settingsAction("night")
	a.settingsAction("clock")
	if !a.night || a.cfg.Use24h() == wasH24 {
		t.Fatalf("night=%v clock24=%v (was %v)", a.night, a.cfg.Use24h(), wasH24)
	}
	b := testApp(t, dir) // a fresh start reads the saved choices
	b.loadState()
	if !b.night || b.cfg.Use24h() == wasH24 {
		t.Errorf("not remembered: night=%v clock24=%v", b.night, b.cfg.Use24h())
	}
}

func TestSettingsZonesExist(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.view = ViewSettings
	_, hits := Render(a.scene(a.cfg.nowIn()))
	want := map[string]bool{"fl_up": false, "fl_down": false, "fl:24": false, "fl:0": false, "warm_up": false,
		"night": false, "clock": false, "full": false, "sync": false, "view:home": false, "exit": false}
	for _, h := range hits {
		if _, ok := want[h.Action]; ok {
			want[h.Action] = true
		}
	}
	for act, found := range want {
		if !found {
			t.Errorf("no tap zone for %q on the Settings screen", act)
		}
	}
}

func TestNightModeKeepsAlbumArt(t *testing.T) {
	c := NewCanvas(400, 300)
	art := Rect{100, 100, 100, 100}
	c.Fill(Rect{0, 0, 400, 300}, 200)
	c.Fill(art, 50)
	invertCanvas(c, []Rect{art})
	if got := c.Img.GrayAt(10, 10).Y; got != 55 {
		t.Errorf("background %d, want 55", got)
	}
	if got := c.Img.GrayAt(150, 150).Y; got != 50 {
		t.Errorf("album art %d, want it left as it was (50)", got)
	}
}
