package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// LibKind says what tapping a library row does.
type LibKind int

const (
	KindContext LibKind = iota // play a playlist or album from its start
	KindTrack                  // play this track, then the ones after it in the list
	KindDevice                 // move playback to this device
)

// LibItem is one row of the library browser.
type LibItem struct {
	Title, Sub string
	URI        string
	Kind       LibKind
	DeviceID   string
	Active     bool // for devices: this one is playing now
}

type apiArtist struct {
	Name string `json:"name"`
}

func artistNames(as []apiArtist) string {
	names := make([]string, 0, len(as))
	for _, a := range as {
		names = append(names, a.Name)
	}
	return strings.Join(names, ", ")
}

type apiTrack struct {
	Name    string      `json:"name"`
	URI     string      `json:"uri"`
	Artists []apiArtist `json:"artists"`
	Album   struct {
		Name string `json:"name"`
	} `json:"album"`
}

func (t apiTrack) item() LibItem {
	sub := artistNames(t.Artists)
	if t.Album.Name != "" {
		sub += "  ·  " + t.Album.Name
	}
	return LibItem{Title: t.Name, Sub: sub, URI: t.URI, Kind: KindTrack}
}

// pages follows Spotify's "next" links for at most maxPages pages of 50.
func (s *Spotify) pages(path string, q url.Values, maxPages int, each func(raw json.RawMessage)) error {
	q.Set("limit", "50")
	for page := 0; page < maxPages; page++ {
		q.Set("offset", fmt.Sprint(page*50))
		_, body, err := s.request("GET", path, q, nil)
		if err != nil {
			return err
		}
		var resp struct {
			Items []json.RawMessage `json:"items"`
			Next  string            `json:"next"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return err
		}
		for _, it := range resp.Items {
			each(it)
		}
		if resp.Next == "" {
			break
		}
	}
	return nil
}

// Playlists lists your playlists (the ones you made and the ones you follow).
func (s *Spotify) Playlists() ([]LibItem, error) {
	var out []LibItem
	err := s.pages("/me/playlists", url.Values{}, 2, func(raw json.RawMessage) {
		var p struct {
			Name  string `json:"name"`
			URI   string `json:"uri"`
			Owner struct {
				Name string `json:"display_name"`
			} `json:"owner"`
		}
		if json.Unmarshal(raw, &p) == nil && p.URI != "" {
			out = append(out, LibItem{Title: p.Name, Sub: p.Owner.Name, URI: p.URI, Kind: KindContext})
		}
	})
	return out, err
}

// Albums lists the albums you saved.
func (s *Spotify) Albums() ([]LibItem, error) {
	var out []LibItem
	err := s.pages("/me/albums", url.Values{}, 2, func(raw json.RawMessage) {
		var a struct {
			Album struct {
				Name    string      `json:"name"`
				URI     string      `json:"uri"`
				Artists []apiArtist `json:"artists"`
			} `json:"album"`
		}
		if json.Unmarshal(raw, &a) == nil && a.Album.URI != "" {
			out = append(out, LibItem{Title: a.Album.Name, Sub: artistNames(a.Album.Artists), URI: a.Album.URI, Kind: KindContext})
		}
	})
	return out, err
}

// Liked lists your most recently liked songs.
func (s *Spotify) Liked() ([]LibItem, error) {
	var out []LibItem
	err := s.pages("/me/tracks", url.Values{}, 1, func(raw json.RawMessage) {
		var t struct {
			Track apiTrack `json:"track"`
		}
		if json.Unmarshal(raw, &t) == nil && t.Track.URI != "" {
			out = append(out, t.Track.item())
		}
	})
	return out, err
}

// Recent lists what you played lately, without repeats.
func (s *Spotify) Recent() ([]LibItem, error) {
	var out []LibItem
	seen := map[string]bool{}
	err := s.pages("/me/player/recently-played", url.Values{}, 1, func(raw json.RawMessage) {
		var t struct {
			Track apiTrack `json:"track"`
		}
		if json.Unmarshal(raw, &t) == nil && t.Track.URI != "" && !seen[t.Track.URI] {
			seen[t.Track.URI] = true
			out = append(out, t.Track.item())
		}
	})
	return out, err
}

// Devices lists the Spotify players that are on right now.
func (s *Spotify) Devices() ([]LibItem, error) {
	_, body, err := s.request("GET", "/me/player/devices", nil, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Devices []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Type         string `json:"type"`
			IsActive     bool   `json:"is_active"`
			IsRestricted bool   `json:"is_restricted"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var out []LibItem
	for _, d := range resp.Devices {
		if d.ID == "" || d.IsRestricted {
			continue
		}
		out = append(out, LibItem{Title: d.Name, Sub: d.Type, Kind: KindDevice, DeviceID: d.ID, Active: d.IsActive})
	}
	return out, nil
}

// play starts playback; when no device is active it picks the first one Spotify can see.
func (s *Spotify) play(body map[string]any) error {
	_, _, err := s.request("PUT", "/me/player/play", nil, body)
	var se *spotifyErr
	if errors.As(err, &se) && se.reason == "NO_ACTIVE_DEVICE" {
		devs, derr := s.Devices()
		if derr != nil || len(devs) == 0 {
			return err
		}
		_, _, err = s.request("PUT", "/me/player/play", url.Values{"device_id": {devs[0].DeviceID}}, body)
	}
	return err
}

// PlayContext plays a playlist or album from its start.
func (s *Spotify) PlayContext(uri string) error {
	return s.play(map[string]any{"context_uri": uri})
}

// PlayTracks plays the given tracks in order (Spotify accepts up to 100).
func (s *Spotify) PlayTracks(uris []string) error {
	if len(uris) > 100 {
		uris = uris[:100]
	}
	return s.play(map[string]any{"uris": uris})
}

// Transfer moves playback to a device, keeping it playing if it was.
func (s *Spotify) Transfer(deviceID string, keepPlaying bool) error {
	_, _, err := s.request("PUT", "/me/player", nil, map[string]any{"device_ids": []string{deviceID}, "play": keepPlaying})
	return err
}
