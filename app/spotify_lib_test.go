package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSpotify struct {
	mu       sync.Mutex
	playBody []string // bodies of PUT /me/player/play, with "?device" noted
	transfer string
	noDevice bool // first play fails with NO_ACTIVE_DEVICE
	scope403 bool
	seeks    []string // position_ms of each PUT /me/player/seek
}

func (f *fakeSpotify) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scope403 {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error":{"status":403,"message":"Insufficient client scope"}}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	switch {
	case r.URL.Path == "/me/playlists":
		fmt.Fprint(w, `{"items":[{"name":"Morning","uri":"spotify:playlist:p1","owner":{"display_name":"Sam"}},
		  {"name":"Road trip","uri":"spotify:playlist:p2","owner":{"display_name":"A Friend"}}],"next":null}`)
	case r.URL.Path == "/me/albums":
		fmt.Fprint(w, `{"items":[{"album":{"name":"Album One","uri":"spotify:album:a1","artists":[{"name":"X"},{"name":"Y"}]}}]}`)
	case r.URL.Path == "/me/tracks":
		fmt.Fprint(w, `{"items":[{"track":{"name":"T1","uri":"spotify:track:t1","artists":[{"name":"X"}],"album":{"name":"Album One"}}},
		  {"track":{"name":"T2","uri":"spotify:track:t2","artists":[{"name":"Z"}],"album":{"name":"Other"}}}]}`)
	case r.URL.Path == "/me/player/recently-played":
		fmt.Fprint(w, `{"items":[{"track":{"name":"T1","uri":"spotify:track:t1","artists":[{"name":"X"}],"album":{"name":"A"}}},
		  {"track":{"name":"T1","uri":"spotify:track:t1","artists":[{"name":"X"}],"album":{"name":"A"}}}]}`)
	case r.URL.Path == "/me/player/devices":
		fmt.Fprint(w, `{"devices":[{"id":"d1","name":"Living room","type":"Speaker","is_active":false},
		  {"id":"d2","name":"Phone","type":"Smartphone","is_active":true},
		  {"id":"d3","name":"Locked","type":"TV","is_restricted":true}]}`)
	case r.URL.Path == "/me/player/seek" && r.Method == "PUT":
		f.seeks = append(f.seeks, r.URL.Query().Get("position_ms"))
		w.WriteHeader(204)
	case r.URL.Path == "/me/player/play":
		f.playBody = append(f.playBody, r.URL.Query().Get("device_id")+"|"+string(body))
		if f.noDevice && r.URL.Query().Get("device_id") == "" {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"status":404,"message":"Player command failed: No active device found","reason":"NO_ACTIVE_DEVICE"}}`)
			return
		}
		w.WriteHeader(204)
	case r.URL.Path == "/me/player" && r.Method == "PUT":
		f.transfer = string(body)
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}

func newTestSpotify(t *testing.T, f *fakeSpotify) *Spotify {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := spotifyAPI
	spotifyAPI = srv.URL
	t.Cleanup(func() { spotifyAPI = old })

	dir := t.TempDir()
	tok, _ := json.Marshal(tokenFile{AccessToken: "x", RefreshToken: "y", ExpiresAt: float64(time.Now().Add(time.Hour).Unix())})
	if err := os.WriteFile(filepath.Join(dir, "spotify_token.json"), tok, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{SpotifyClientID: "id", Dir: dir}
	cfg.applyDefaults()
	return NewSpotify(cfg)
}

func TestLibraryLists(t *testing.T) {
	s := newTestSpotify(t, &fakeSpotify{})
	pl, err := s.Playlists()
	if err != nil || len(pl) != 2 || pl[0].Title != "Morning" || pl[1].Sub != "A Friend" || pl[0].Kind != KindContext {
		t.Fatalf("playlists: %+v %v", pl, err)
	}
	al, _ := s.Albums()
	if len(al) != 1 || al[0].Sub != "X, Y" || al[0].URI != "spotify:album:a1" {
		t.Errorf("albums: %+v", al)
	}
	liked, _ := s.Liked()
	if len(liked) != 2 || liked[0].Kind != KindTrack || liked[1].Sub != "Z  ·  Other" {
		t.Errorf("liked: %+v", liked)
	}
	recent, _ := s.Recent()
	if len(recent) != 1 {
		t.Errorf("recent should drop repeats: %+v", recent)
	}
	devs, _ := s.Devices()
	if len(devs) != 2 || !devs[1].Active || devs[0].DeviceID != "d1" {
		t.Errorf("devices should skip restricted ones: %+v", devs)
	}
}

func TestPlayAndFallbackToFirstDevice(t *testing.T) {
	f := &fakeSpotify{noDevice: true}
	s := newTestSpotify(t, f)
	if err := s.PlayContext("spotify:playlist:p1"); err != nil {
		t.Fatal(err)
	}
	if len(f.playBody) != 2 || !strings.HasPrefix(f.playBody[0], "|") || !strings.HasPrefix(f.playBody[1], "d1|") {
		t.Fatalf("expected a failed try then a retry on device d1: %v", f.playBody)
	}
	if !strings.Contains(f.playBody[1], `"context_uri":"spotify:playlist:p1"`) {
		t.Errorf("body: %s", f.playBody[1])
	}
	f.playBody = nil
	f.noDevice = false
	if err := s.PlayTracks([]string{"spotify:track:t1", "spotify:track:t2"}); err != nil {
		t.Fatal(err)
	}
	if len(f.playBody) != 1 || !strings.Contains(f.playBody[0], `"uris":["spotify:track:t1","spotify:track:t2"]`) {
		t.Errorf("tracks body: %v", f.playBody)
	}
	if err := s.Transfer("d2", true); err != nil || !strings.Contains(f.transfer, `"device_ids":["d2"]`) || !strings.Contains(f.transfer, `"play":true`) {
		t.Errorf("transfer: %q %v", f.transfer, err)
	}
}

func TestScopeErrorIsExplained(t *testing.T) {
	s := newTestSpotify(t, &fakeSpotify{scope403: true})
	_, err := s.Playlists()
	if err == nil || err.Error() != needScope {
		t.Fatalf("want the new-permission message, got %v", err)
	}
}
