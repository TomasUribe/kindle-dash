package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseLRC(t *testing.T) {
	// invented text; only the format matters here
	got := parseLRC("[ar:Someone]\n[00:12.50] first made-up line\n[01:05.00]second made-up line\n[00:30.00][02:00.25] repeated chorus line\n[03:00.00]   \nnot a lyric line\n")
	want := []struct {
		at   time.Duration
		text string
	}{
		{12500 * time.Millisecond, "first made-up line"},
		{30 * time.Second, "repeated chorus line"},
		{65 * time.Second, "second made-up line"},
		{120250 * time.Millisecond, "repeated chorus line"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d lines, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].At != w.at || got[i].Text != w.text {
			t.Errorf("line %d: %v %q, want %v %q", i, got[i].At, got[i].Text, w.at, w.text)
		}
	}
}

func TestCurrentIndex(t *testing.T) {
	l := &Lyrics{Found: true, Synced: true, Lines: []LyricLine{{At: 5 * time.Second}, {At: 10 * time.Second}, {At: 20 * time.Second}}}
	for pos, want := range map[time.Duration]int{0: -1, 4999 * time.Millisecond: -1, 5 * time.Second: 0, 15 * time.Second: 1, 99 * time.Second: 2} {
		if got := l.CurrentIndex(pos); got != want {
			t.Errorf("at %v: %d, want %d", pos, got, want)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Married with Children - Remastered":        "Married with Children",
		"Song Name (Remastered 2011)":               "Song Name",
		"Song Name - 2009 Remaster":                 "Song Name",
		"Shine On You Crazy Diamond (Pts. 6-9)":     "Shine On You Crazy Diamond (Pts. 6-9)",
		"Plain Title":                               "Plain Title",
		"Another One - Live at Somewhere":           "Another One",
		"Something (feat. A Friend) - Radio Edit":   "Something (feat. A Friend)",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// A fake lyrics service with invented text: exact match, a title it only knows when cleaned, search fallback.
func TestFetchLyricsFallbacks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/get":
			if q.Get("track_name") == "Known Song" {
				fmt.Fprint(w, `{"trackName":"Known Song","duration":200,"syncedLyrics":"[00:01.00] invented one\n[00:05.00] invented two"}`)
				return
			}
			http.NotFound(w, r)
		case "/api/search":
			if q.Get("track_name") == "Only Searchable" {
				fmt.Fprint(w, `[{"trackName":"Only Searchable","duration":400,"plainLyrics":"too long, wrong version"},
				                {"trackName":"Only Searchable","duration":181,"plainLyrics":"invented plain A\ninvented plain B"}]`)
				return
			}
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	old := lyricsBase
	lyricsBase = srv.URL + "/api"
	defer func() { lyricsBase = old }()

	l, err := FetchLyrics("Known Song - Remastered 2011", "Someone", "Album", 200*time.Second)
	if err != nil || !l.Found || !l.Synced || len(l.Lines) != 2 {
		t.Fatalf("cleaned lookup: %+v %v", l, err)
	}
	l, err = FetchLyrics("Only Searchable", "Someone", "Album", 180*time.Second)
	if err != nil || !l.Found || l.Synced || len(l.Lines) != 2 || l.Lines[0].Text != "invented plain A" {
		t.Fatalf("search fallback should pick the version of matching length: %+v %v", l, err)
	}
	l, err = FetchLyrics("Nothing Here", "Someone", "Album", 100*time.Second)
	if err != nil || l.Found {
		t.Fatalf("unknown track: %+v %v", l, err)
	}
}

// The redraw must be scheduled so that a line is on screen before it is sung.
func TestLyricsRedrawIsScheduledEarly(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.cfg.LyricsLeadSeconds = 2.5
	a.view, a.lyrHL = ViewLyrics, true
	now := time.Now()
	a.player = Player{Active: true, Playing: true, Title: "T", Artist: "A", Album: "B", Dur: 200 * time.Second,
		Progress: 10 * time.Second, FetchedAt: now}
	a.lyrCache[lyricsKey(a.player)] = &Lyrics{Found: true, Synced: true, Lines: []LyricLine{
		{At: 5 * time.Second, Text: "one"}, {At: 20 * time.Second, Text: "two"}, {At: 30 * time.Second, Text: "three"}}}

	// at 10 s with a 2.5 s lead the highlight sees 12.5 s; line "two" (at 20 s) is due in 7.5 s
	d, ok := a.lyricsDelay(now)
	if !ok {
		t.Fatal("a playing, synced song on the lyrics screen needs a timer")
	}
	want := 7500*time.Millisecond - settleDelay + 80*time.Millisecond
	if diff := d - want; diff < -50*time.Millisecond || diff > 50*time.Millisecond {
		t.Errorf("delay %v, want about %v", d, want)
	}
	// a line that is already due is retried soon, not spun on
	a.player.Progress = 17600 * time.Millisecond // position 20.1 s: "two" is current, "three" is 9.9 s away
	if d, _ := a.lyricsDelay(now); d < 9*time.Second || d > 10*time.Second {
		t.Errorf("after the line changed the next delay should be about 9.5 s, got %v", d)
	}
	// nothing to schedule when paused, on another screen, or after the last line
	a.player.Playing = false
	if _, ok := a.lyricsDelay(now); ok {
		t.Error("paused music needs no lyrics timer")
	}
	a.player.Playing, a.view = true, ViewHome
	if _, ok := a.lyricsDelay(now); ok {
		t.Error("no lyrics timer off the lyrics screen")
	}
	a.view, a.player.Progress = ViewLyrics, 100*time.Second
	if _, ok := a.lyricsDelay(now); ok {
		t.Error("no timer after the last line")
	}
	// with the line marker off there is nothing to redraw per line, only page turns
	a.player.Progress, a.lyrHL = 10*time.Second, false
	if _, ok := a.lyricsDelay(now); ok {
		t.Error("no marker and no page turn pending: no timer needed")
	}
	a.lyrTurns = []time.Duration{40 * time.Second}
	if d, ok := a.lyricsDelay(now); !ok || d < 19*time.Second {
		t.Errorf("a page turn 30 s away should set a timer (capped at 20 s), got %v %v", d, ok)
	}
	a.lyrAuto = false
	if _, ok := a.lyricsDelay(now); ok {
		t.Error("when not following, page turns do not matter")
	}
}

// lines of invented text, one every `every`, so the plan has several pages
func spacedLyrics(n int, every time.Duration) *Lyrics {
	l := &Lyrics{Found: true, Synced: true}
	for i := 0; i < n; i++ {
		l.Lines = append(l.Lines, LyricLine{At: time.Duration(i+1) * every, Text: fmt.Sprintf("invented line number %d", i)})
	}
	return l
}

func TestPageTurnsWaitForTheLastLineOfThePage(t *testing.T) {
	// 12 one-row lines, 4 per page
	plan := lyricsPlan{perPage: 4, pages: 3}
	for i := 0; i < 12; i++ {
		plan.vl = append(plan.vl, vline{text: "x", idx: i})
	}
	l := &Lyrics{Found: true, Synced: true}
	times := []float64{1, 6, 11, 16, 20, 25, 30, 35, 36, 38, 60, 65} // line 4 (first of page 1) at 20 s, line 8 at 36 s, a 2 s gap before line 10
	for _, s := range times {
		l.Lines = append(l.Lines, LyricLine{At: time.Duration(s * float64(time.Second))})
	}
	turns := plan.pageTurns(l, time.Second)
	if len(turns) != 2 {
		t.Fatalf("turns: %v", turns)
	}
	// page 1 starts at line 4 (20 s). One second ahead is 19 s, which is after 70% of line 3's slot (16..20 s = 18.8 s).
	if turns[0] != 19*time.Second {
		t.Errorf("page 1 should come up 1 s before its first line, got %v", turns[0])
	}
	// page 2 starts at line 8 (36 s); line 7 is only sung from 35 s, so the turn waits until 70% of that 1 s slot, not 35 s
	if want := 35*time.Second + 700*time.Millisecond; turns[1] != want {
		t.Errorf("page 2 should wait for line 7 to be nearly done: got %v, want %v", turns[1], want)
	}
	for pos, want := range map[time.Duration]int{0: 0, 18900 * time.Millisecond: 0, 19 * time.Second: 1, 35600 * time.Millisecond: 1, 35700 * time.Millisecond: 2, 99 * time.Second: 2} {
		if got := pageAt(turns, pos); got != want {
			t.Errorf("at %v: page %d, want %d", pos, got, want)
		}
	}
	// a long gap: the old page stays up through the instrumental, the new one still arrives 1 s ahead
	l.Lines[4].At = 40 * time.Second // page 1 now starts at 40 s
	if got := plan.pageTurns(l, time.Second)[0]; got != 39*time.Second {
		t.Errorf("after a long gap the turn is still 1 s before the first line: %v", got)
	}
	// the page lead never exceeds the highlight lead
	cfg := &Config{LyricsLeadSeconds: 2.5}
	if lyricsPageLead(cfg) != time.Second {
		t.Errorf("page lead should be capped at 1 s, got %v", lyricsPageLead(cfg))
	}
	cfg.LyricsLeadSeconds = 0.5
	if lyricsPageLead(cfg) != 500*time.Millisecond {
		t.Errorf("page lead should not exceed the lead: %v", lyricsPageLead(cfg))
	}
}

// Following the song must not turn away from a page while its last line is still being sung, and the
// marker, which runs ahead of the singer, must stay on the page that is showing.
func TestFollowingDoesNotCutOffTheLastLines(t *testing.T) {
	cfg := DemoConfig()
	cfg.LyricsLeadSeconds = 2.5
	l := spacedLyrics(60, 5*time.Second) // a line every 5 s from 5 s
	now := time.Now()
	scene := func(pos time.Duration) *Scene {
		return &Scene{Now: now, Cfg: cfg, View: ViewLyrics, Orient: Portrait,
			Player: Player{Active: true, Playing: false, Title: "T", Artist: "A", Dur: 400 * time.Second, Progress: pos, FetchedAt: now},
			Lyrics: LyricsView{L: l, Auto: true, Size: 48, Highlight: true}, Out: &SceneOut{}}
	}
	s0 := scene(0)
	Render(s0)
	if s0.Out.LyrPages < 3 || len(s0.Out.LyrTurns) != s0.Out.LyrPages-1 {
		t.Fatalf("expected several pages with a turn time each: %+v", s0.Out)
	}
	turn := s0.Out.LyrTurns[0]
	// the first line of page 1 is due 1 s after the turn; line i is at 5*(i+1) s, so that is line (turn+1s)/5s - 1
	firstOfNext := int((turn+time.Second)/(5*time.Second)) - 1
	lastOfThis := firstOfNext - 1
	sungFrom := l.Lines[lastOfThis].At

	// 1 s before the turn the last line of the page has been sung for most of its slot, yet the page is still page 0
	// and the marker (2.5 s ahead) is held on the last line of that page rather than on the next page's first line.
	s := scene(turn - 100*time.Millisecond)
	Render(s)
	if s.Out.LyrPage != 0 {
		t.Errorf("page turned too early, at %v", turn-100*time.Millisecond)
	}
	if s.Out.LyrLine != lastOfThis {
		t.Errorf("marker should wait on line %d (the last of the page), got %d", lastOfThis, s.Out.LyrLine)
	}
	if turn < sungFrom+3*time.Second {
		t.Errorf("the page turned only %v into a 5 s line", turn-sungFrom)
	}
	s = scene(turn)
	Render(s)
	if s.Out.LyrPage != 1 {
		t.Errorf("page should have turned at %v, still on %d", turn, s.Out.LyrPage)
	}
}

// The highlight runs this far ahead by default; 2.5 s felt slightly too early on the real screen.
func TestDefaultLyricsLead(t *testing.T) {
	cfg := DemoConfig()
	if got := lyricsLead(cfg); got != 2*time.Second {
		t.Errorf("default lead %v, want 2s", got)
	}
	if got := lyricsPageLead(cfg); got != time.Second {
		t.Errorf("page lead %v, want 1s", got)
	}
}
