package main

import (
	"errors"
	"image"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSpotifyNextTrackFromQueue(t *testing.T) {
	s := newTestSpotify(t, &fakeSpotify{})
	p, ok, err := s.NextTrack(time.Now())
	if err != nil || !ok {
		t.Fatalf("next: %v %v", ok, err)
	}
	if p.Title != "Up Next" || p.Artist != "X, Y" || p.Album != "The Album" || p.Dur != 181*time.Second ||
		p.ArtURL != "https://img.example/big.jpg" || p.TrackURI != "spotify:track:next" || !p.Active {
		t.Errorf("next track: %+v", p)
	}
	// an unreachable queue is an error, not a made-up song
	if _, _, err := newTestSpotify(t, &fakeSpotify{scope403: true}).NextTrack(time.Now()); err == nil {
		t.Error("a failing queue request should be reported")
	}
}

func TestArtCacheKeepsSeveralCoversAndNotifiesEveryone(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	release := make(chan struct{})
	old := fetchArt
	fetchArt = func(url string) (image.Image, error) {
		mu.Lock()
		calls[url]++
		mu.Unlock()
		if url == "slow" {
			<-release
		}
		if url == "broken" {
			return nil, errors.New("no")
		}
		return gray(8, 8, 120), nil
	}
	defer func() { fetchArt = old }()

	c := NewArtCache()
	wait := func(url string) {
		deadline := time.After(2 * time.Second)
		for c.Get(url, 4) == nil {
			select {
			case <-deadline:
				t.Fatalf("%s never loaded", url)
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	c.Request("a", nil)
	wait("a")
	c.Request("b", nil) // warming the next cover must not blank the current one
	wait("b")
	if c.Get("a", 4) == nil || c.Get("b", 4) == nil {
		t.Fatal("both covers should be available")
	}

	// a request for a cover that is already on its way still gets its callback
	var told atomic.Int32
	c.Request("slow", nil) // the warm-up: nobody to tell
	time.Sleep(20 * time.Millisecond)
	c.Request("slow", func() { told.Add(1) }) // the screen asks for it when the song changes
	close(release)
	wait("slow")
	time.Sleep(20 * time.Millisecond)
	if told.Load() != 1 {
		t.Errorf("late requester was told %d times, want 1", told.Load())
	}
	mu.Lock()
	if calls["slow"] != 1 {
		t.Errorf("one download expected, got %d", calls["slow"])
	}
	mu.Unlock()

	// only a few are kept: the oldest goes
	c.Request("d", nil)
	wait("d")
	if c.Get("a", 4) != nil {
		t.Error("the oldest cover should have been dropped")
	}
	// a failed download is not retried at once
	c.Request("broken", nil)
	time.Sleep(50 * time.Millisecond)
	c.Request("broken", nil)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if calls["broken"] != 1 {
		t.Errorf("failed cover fetched %d times, want 1 for now", calls["broken"])
	}
}

// runTasks executes what the app posted to its loop until cond holds.
func runTasks(t *testing.T, a *App, cond func() bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for !cond() {
		select {
		case fn := <-a.tasks:
			fn()
		case <-deadline:
			t.Fatal("the app never got there")
		}
	}
}

// nearEnd puts the demo player `left` before the end of track idx.
func nearEnd(a *App, idx int, left time.Duration) {
	a.dp.mu.Lock()
	a.dp.idx, a.dp.pos, a.dp.stamp, a.dp.playing = idx, demoTracks[idx].dur-left, time.Now(), true
	a.dp.mu.Unlock()
	a.setPlayer(a.dp.State(time.Now()))
}

func TestNextSongIsShownBeforeItStarts(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.view = ViewMusic
	nearEnd(a, 3, 12*time.Second)
	key := a.lyricsKeyNow()
	defer func() {
		if a.nextTimer != nil {
			a.nextTimer.Stop()
		}
	}()

	a.draw(true) // 12 s from the end: inside the look-ahead, so the next song is looked up
	runTasks(t, a, func() bool { return a.nextFor == key })
	want := demoTracks[4]
	if a.nextTrack.Title != want.title {
		t.Fatalf("next song %q, want %q", a.nextTrack.Title, want.title)
	}
	// its cover was fetched ahead of time
	deadline := time.After(2 * time.Second)
	for a.art.Get(a.nextTrack.ArtURL, 50) == nil {
		select {
		case <-deadline:
			t.Fatal("the next cover was never warmed")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if a.nextTimer == nil {
		a.draw(false) // the timer is armed after the next draw
		if a.nextTimer == nil {
			t.Fatal("no timer to show the next song early")
		}
	}

	// the moment comes: the next song goes up, counting up from before zero (the time left in the old song)
	a.showNextEarly(key)
	if a.player.Title != want.title || a.player.Dur != want.dur {
		t.Fatalf("expected the next song on screen: %+v", a.player)
	}
	if got := a.player.Expected(time.Now()); got > -10*time.Second || got < -12*time.Second {
		t.Fatalf("the countdown should be the ~12 s left in the old song, got %v", got)
	}
	if got := a.player.Expected(time.Now().Add(13 * time.Second)); got < 500*time.Millisecond || got > 2500*time.Millisecond {
		t.Errorf("the song should be about 1 s in once the old one ends, got %v", got)
	}

	// Spotify still reporting the old song must not flip the screen back
	a.applyPlayer(Player{Active: true, Playing: true, Title: demoTracks[3].title, Artist: demoTracks[3].artist,
		Album: demoTracks[3].album, Dur: demoTracks[3].dur, Progress: demoTracks[3].dur - 2*time.Second, FetchedAt: time.Now()})
	if a.player.Title != want.title {
		t.Errorf("an old-song answer replaced the early next song with %q", a.player.Title)
	}
	// once Spotify reports the new song it takes over, with the real position
	nearEnd(a, 4, want.dur-2500*time.Millisecond)
	a.applyPlayer(a.dp.State(time.Now()))
	if a.player.Title != want.title || a.player.Expected(time.Now()) < 2*time.Second || a.early.from != "" {
		t.Errorf("the real answer should take over: %+v early=%+v", a.player, a.early)
	}
}

func TestNextSongLookupIsLeftAloneWhenNotNeeded(t *testing.T) {
	cases := map[string]func(a *App){
		"far from the end": func(a *App) { nearEnd(a, 3, 90*time.Second) },
		"paused": func(a *App) {
			nearEnd(a, 3, 10*time.Second)
			a.player.Playing = false
		},
		"music not on screen": func(a *App) {
			nearEnd(a, 3, 10*time.Second)
			a.view = ViewCalendar
		},
		"switched off": func(a *App) {
			nearEnd(a, 3, 10*time.Second)
			a.cfg.NextTrackLead = -1
		},
	}
	for name, setup := range cases {
		a := testApp(t, t.TempDir())
		a.view = ViewMusic
		setup(a)
		a.prepareNext()
		if a.nextBusy || a.nextFor != "" || a.nextTimer != nil {
			t.Errorf("%s: should not look anything up", name)
		}
	}
}

func TestDefaultNextTrackLead(t *testing.T) {
	if got := DemoConfig().NextTrackLead; got != 4 {
		t.Errorf("default lead %v, want 4 s", got)
	}
}

// The progress in Spotify's answer belongs to the moment it was built, about halfway through the request;
// dating it from the start of a slow request would make the song look behind by the whole delay.
func TestPlayerStateIsDatedHalfwayThroughTheRequest(t *testing.T) {
	s := newTestSpotify(t, &fakeSpotify{delay: 400 * time.Millisecond})
	t0 := time.Now()
	p := s.State(t0)
	t1 := time.Now()
	if p.Err != "" || p.Title != "T" || p.Progress != 60*time.Second {
		t.Fatalf("state: %+v", p)
	}
	mid := t0.Add(t1.Sub(t0) / 2)
	if d := p.FetchedAt.Sub(mid); d < -60*time.Millisecond || d > 60*time.Millisecond {
		t.Errorf("fetched-at %v is %v away from the middle of the request", p.FetchedAt.Sub(t0), d)
	}
	if p.FetchedAt.Sub(t0) < 150*time.Millisecond {
		t.Errorf("still dated from the start of the request: %v after it began", p.FetchedAt.Sub(t0))
	}
}

// When Spotify will not say what is next, the app asks right after the song should end and keeps asking
// until it changes, instead of waiting for the next regular poll.
func TestEndOfSongPollingNoticesTheNewSong(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.view = ViewMusic
	nearEnd(a, 3, 700*time.Millisecond)
	key := a.lyricsKeyNow()
	start := time.Now()
	go a.pollUntilChanged(key)
	runTasks(t, a, func() bool { return a.lyricsKeyNow() != key })
	if a.player.Title != demoTracks[4].title {
		t.Errorf("expected the next demo song, got %q", a.player.Title)
	}
	if took := time.Since(start); took > 2500*time.Millisecond {
		t.Errorf("the change took %v to notice (one retry should be enough)", took)
	}
	deadline := time.Now().Add(2 * time.Second)
	for a.endBusy.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.endBusy.Load() {
		t.Error("the polling run should have finished")
	}
}

func TestEndOfSongPollIsArmedOnlyWhenItMakesSense(t *testing.T) {
	cases := map[string]struct {
		setup func(a *App)
		armed bool
	}{
		"playing, music on screen": {func(a *App) { nearEnd(a, 3, 60*time.Second) }, true},
		"paused":                   {func(a *App) { nearEnd(a, 3, 60*time.Second); a.player.Playing = false }, false},
		"music not on screen":      {func(a *App) { nearEnd(a, 3, 60*time.Second); a.view = ViewSettings }, false},
		"unknown length":           {func(a *App) { nearEnd(a, 3, 60*time.Second); a.player.Dur = 0 }, false},
		"nothing playing":          {func(a *App) { a.player = Player{} }, false},
	}
	for name, c := range cases {
		a := testApp(t, t.TempDir())
		a.view = ViewMusic
		c.setup(a)
		a.armEndPoll()
		if got := a.endTimer != nil; got != c.armed {
			t.Errorf("%s: armed=%v, want %v", name, got, c.armed)
		}
		if a.endTimer != nil {
			a.endTimer.Stop()
		}
		if a.nextTimer != nil {
			a.nextTimer.Stop()
		}
	}
}
