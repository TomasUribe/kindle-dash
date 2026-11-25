package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const volumeStep = 10

var (
	spotifyAPI   = "https://api.spotify.com/v1"
	spotifyToken = "https://accounts.spotify.com/api/token"
)

// needScope is shown when the saved Spotify login predates a feature that needs more permission.
const needScope = "Spotify needs new permission for this. Run tools/spotify_login.py on the computer, then install again."

var friendly = map[string]string{
	"NO_ACTIVE_DEVICE":        "No active Spotify device. Start playing in the Spotify app first.",
	"PREMIUM_REQUIRED":        "Spotify Premium is required to control playback.",
	"VOLUME_CONTROL_DISALLOW": "This device does not allow remote volume control.",
}

type tokenFile struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresAt    float64 `json:"expires_at"`
	Scope        string  `json:"scope"`
}

type spotifyErr struct {
	msg, reason string
	status      int
}

func (e *spotifyErr) Error() string { return e.msg }

// Spotify talks to the Web API directly from the Kindle: no server in between.
// The token file comes from tools/spotify_login.py on a computer; this program only refreshes it.
type Spotify struct {
	cfg    *Config
	client *http.Client

	mu      sync.Mutex
	tok     tokenFile
	loaded  bool
	backoff time.Time
}

func NewSpotify(cfg *Config) *Spotify {
	return &Spotify{cfg: cfg, client: &http.Client{Timeout: 20 * time.Second}}
}

// Available reports whether a login has been set up.
func (s *Spotify) Available() bool {
	if s.cfg.SpotifyClientID == "" {
		return false
	}
	_, err := os.Stat(s.cfg.SpotifyTokenFile)
	return err == nil
}

// State reads what is playing right now. It never sends a command.
func (s *Spotify) State(now time.Time) Player {
	status, body, err := s.request("GET", "/me/player", url.Values{"additional_types": {"episode"}}, nil)
	if err != nil {
		return Player{Err: err.Error(), Volume: -1, Hint: "Spotify unavailable", FetchedAt: now}
	}
	return normalize(status, body, now)
}

// Act runs one control action and returns the fresh state.
func (s *Spotify) Act(action string, now time.Time) (Player, error) {
	var err error
	switch action {
	case "toggle":
		err = s.toggle(now)
	case "next":
		_, _, err = s.request("POST", "/me/player/next", nil, nil)
	case "prev":
		_, _, err = s.request("POST", "/me/player/previous", nil, nil)
	case "vol_up", "vol_down":
		err = s.stepVolume(now, map[string]int{"vol_up": volumeStep, "vol_down": -volumeStep}[action])
	default:
		if ms, ok := strings.CutPrefix(action, "seek:"); ok {
			err = s.Seek(ms)
		} else {
			err = fmt.Errorf("unknown action %q", action)
		}
	}
	time.Sleep(500 * time.Millisecond) // Spotify's state lags a moment behind a command
	p := s.State(time.Now())
	return p, err
}

func (s *Spotify) toggle(now time.Time) error {
	if s.State(now).Playing {
		_, _, err := s.request("PUT", "/me/player/pause", nil, nil)
		return err
	}
	_, _, err := s.request("PUT", "/me/player/play", nil, nil)
	var se *spotifyErr
	if errors.As(err, &se) && se.reason == "NO_ACTIVE_DEVICE" {
		return s.wakeDevice()
	}
	return err
}

// wakeDevice hands playback to the first device Spotify can see when nothing is active.
func (s *Spotify) wakeDevice() error {
	_, body, err := s.request("GET", "/me/player/devices", nil, nil)
	if err != nil {
		return err
	}
	var dev struct {
		Devices []struct {
			ID           string `json:"id"`
			IsRestricted bool   `json:"is_restricted"`
		} `json:"devices"`
	}
	_ = json.Unmarshal(body, &dev)
	for _, d := range dev.Devices {
		if d.ID != "" && !d.IsRestricted {
			_, _, err := s.request("PUT", "/me/player", nil, map[string]any{"device_ids": []string{d.ID}, "play": true})
			return err
		}
	}
	return &spotifyErr{msg: friendly["NO_ACTIVE_DEVICE"], reason: "NO_ACTIVE_DEVICE", status: 404}
}

// Seek jumps to a position (milliseconds from the start) in the current song.
func (s *Spotify) Seek(ms string) error {
	n, err := strconv.Atoi(ms)
	if err != nil || n < 0 {
		return fmt.Errorf("bad seek position %q", ms)
	}
	_, _, err = s.request("PUT", "/me/player/seek", url.Values{"position_ms": {strconv.Itoa(n)}}, nil)
	return err
}

func (s *Spotify) stepVolume(now time.Time, delta int) error {
	cur := s.State(now).Volume
	if cur < 0 {
		return &spotifyErr{msg: friendly["VOLUME_CONTROL_DISALLOW"], reason: "VOLUME_CONTROL_DISALLOW", status: 403}
	}
	v := max(0, min(100, cur+delta))
	_, _, err := s.request("PUT", "/me/player/volume", url.Values{"volume_percent": {strconv.Itoa(v)}}, nil)
	return err
}

// --- HTTP and tokens ----------------------------------------------------------

func (s *Spotify) accessToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		raw, err := os.ReadFile(s.cfg.SpotifyTokenFile)
		if err != nil {
			return "", fmt.Errorf("no Spotify login (%v)", err)
		}
		if err := json.Unmarshal(raw, &s.tok); err != nil {
			return "", err
		}
		s.loaded = true
	}
	if s.tok.ExpiresAt-60 > float64(time.Now().Unix()) {
		return s.tok.AccessToken, nil
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.tok.RefreshToken}, "client_id": {s.cfg.SpotifyClientID}}
	resp, err := s.client.PostForm(spotifyToken, form)
	if err != nil {
		return "", errors.New(safeErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("Spotify login expired; run tools/spotify_login.py again")
	}
	var fresh struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fresh); err != nil {
		return "", err
	}
	s.tok.AccessToken = fresh.AccessToken
	s.tok.ExpiresAt = float64(time.Now().Unix() + int64(fresh.ExpiresIn))
	if fresh.RefreshToken != "" {
		s.tok.RefreshToken = fresh.RefreshToken
	}
	if out, err := json.Marshal(s.tok); err == nil {
		_ = os.WriteFile(s.cfg.SpotifyTokenFile, out, 0o600)
	}
	return s.tok.AccessToken, nil
}

func (s *Spotify) request(method, path string, q url.Values, body any) (int, []byte, error) {
	s.mu.Lock()
	wait := time.Until(s.backoff)
	s.mu.Unlock()
	if wait > 0 {
		return 0, nil, errors.New("Spotify is rate limiting requests; retrying shortly")
	}
	tok, err := s.accessToken()
	if err != nil {
		return 0, nil, err
	}
	u := spotifyAPI + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else if method == "PUT" || method == "POST" {
		rd = strings.NewReader("")
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, errors.New("Cannot reach Spotify (" + safeErr(err) + ")")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch {
	case resp.StatusCode == 429:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		s.mu.Lock()
		s.backoff = time.Now().Add(time.Duration(max(secs, 5)) * time.Second)
		s.mu.Unlock()
		return resp.StatusCode, data, errors.New("Spotify is rate limiting requests; retrying shortly")
	case resp.StatusCode >= 400:
		var e struct {
			Error struct {
				Message string `json:"message"`
				Reason  string `json:"reason"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		msg := friendly[e.Error.Reason]
		if msg == "" {
			msg = e.Error.Message
		}
		if strings.Contains(strings.ToLower(e.Error.Message), "scope") {
			msg = needScope
		}
		if msg == "" {
			msg = fmt.Sprintf("Spotify error %d", resp.StatusCode)
		}
		return resp.StatusCode, data, &spotifyErr{msg: msg, reason: e.Error.Reason, status: resp.StatusCode}
	}
	return resp.StatusCode, data, nil
}

type apiImage struct {
	URL   string `json:"url"`
	Width int    `json:"width"`
}

// apiItem is a track or episode as Spotify describes it, in the player state and in the queue.
type apiItem struct {
	Type       string `json:"type"`
	URI        string `json:"uri"`
	Name       string `json:"name"`
	DurationMs int64  `json:"duration_ms"`
	Artists    []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album *struct {
		Name   string     `json:"name"`
		Images []apiImage `json:"images"`
	} `json:"album"`
	Show *struct {
		Name string `json:"name"`
	} `json:"show"`
	Images []apiImage `json:"images"`
}

// fillFromItem copies what is shown about a song (or podcast episode) into the player.
func (p *Player) fillFromItem(it *apiItem) {
	p.Title, p.TrackURI = it.Name, it.URI
	p.Dur = time.Duration(it.DurationMs) * time.Millisecond
	var images []apiImage
	if it.Type == "episode" {
		if it.Show != nil {
			p.Artist = it.Show.Name
		}
		images = it.Images
	} else {
		names := make([]string, 0, len(it.Artists))
		for _, a := range it.Artists {
			names = append(names, a.Name)
		}
		p.Artist = strings.Join(names, ", ")
		if it.Album != nil {
			p.Album = it.Album.Name
			images = it.Album.Images
		}
	}
	if len(images) > 0 {
		p.ArtURL = images[0].URL // largest first
	}
}

// NextTrack asks which song Spotify will play after the current one. ok is false when nothing is queued.
func (s *Spotify) NextTrack(now time.Time) (next Player, ok bool, err error) {
	_, body, err := s.request("GET", "/me/player/queue", nil, nil)
	if err != nil {
		return Player{}, false, err
	}
	var q struct {
		Queue []apiItem `json:"queue"`
	}
	if json.Unmarshal(body, &q) != nil || len(q.Queue) == 0 || q.Queue[0].Name == "" {
		return Player{}, false, nil
	}
	next = Player{Active: true, Playing: true, Volume: -1, FetchedAt: now}
	next.fillFromItem(&q.Queue[0])
	return next, true, nil
}

func normalize(status int, body []byte, now time.Time) Player {
	idle := Player{Volume: -1, Hint: "Start Spotify on a device to see it here", FetchedAt: now}
	if status == 204 || len(body) == 0 {
		return idle
	}
	var d struct {
		IsPlaying            bool   `json:"is_playing"`
		ProgressMs           int64  `json:"progress_ms"`
		CurrentlyPlayingType string `json:"currently_playing_type"`
		Device               *struct {
			Name          string `json:"name"`
			VolumePercent *int   `json:"volume_percent"`
		} `json:"device"`
		Item *apiItem `json:"item"`
	}
	if err := json.Unmarshal(body, &d); err != nil || d.Device == nil {
		return idle
	}
	p := Player{Active: true, Playing: d.IsPlaying, Device: d.Device.Name, Volume: -1,
		Progress: time.Duration(d.ProgressMs) * time.Millisecond, FetchedAt: now}
	if d.Device.VolumePercent != nil {
		p.Volume = *d.Device.VolumePercent
	}
	if d.Item != nil {
		p.fillFromItem(d.Item)
	}
	if p.Title == "" && d.CurrentlyPlayingType == "ad" {
		p.Title = "Advertisement"
	}
	return p
}
