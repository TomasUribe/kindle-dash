package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Spotify has no lyrics service, so lyrics come from LRCLIB (lrclib.net), a free community database.
// It is sent the track name, artist, album and length, and nothing else about you.

var lyricsBase = "https://lrclib.net/api"

type LyricLine struct {
	At   time.Duration // when it starts (synced lyrics only)
	Text string
}

// Lyrics is what the lyrics screen shows.
type Lyrics struct {
	Found        bool
	Instrumental bool
	Synced       bool // lines carry timestamps
	Lines        []LyricLine
}

// CurrentIndex is the line being sung at pos, or -1 before the first line.
func (l *Lyrics) CurrentIndex(pos time.Duration) int {
	if l == nil || !l.Synced {
		return -1
	}
	i := sort.Search(len(l.Lines), func(i int) bool { return l.Lines[i].At > pos })
	return i - 1
}

type lrclibRecord struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

var (
	decorationDash  = regexp.MustCompile(`(?i)\s*[-\x{2013}]\s*(\d{4}\s*)?(remaster(ed)?|live|mono|stereo|single version|radio edit|deluxe|bonus|anniversary).*$`)
	decorationParen = regexp.MustCompile(`(?i)\s*[\(\[]\s*(\d{4}\s*)?(remaster(ed)?|live|mono|stereo|deluxe|bonus|anniversary|single version|radio edit).*?[\)\]]`)
)

// cleanTitle drops "- Remastered 2011" style endings that the lyrics database does not use.
func cleanTitle(t string) string {
	t = decorationDash.ReplaceAllString(t, "")
	t = decorationParen.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

var lrcStamp = regexp.MustCompile(`\[(\d+):(\d+)(?:[.:](\d+))?\]`)

// parseLRC reads "[01:23.45] words" lines; a line may carry several timestamps.
func parseLRC(text string) []LyricLine {
	var out []LyricLine
	for _, raw := range strings.Split(text, "\n") {
		raw = strings.TrimSpace(raw)
		stamps := lrcStamp.FindAllStringSubmatch(raw, -1)
		if len(stamps) == 0 {
			continue // tags like [ar:Artist] or blank lines
		}
		words := strings.TrimSpace(lrcStamp.ReplaceAllString(raw, ""))
		if words == "" {
			continue
		}
		for _, m := range stamps {
			min, _ := strconv.Atoi(m[1])
			sec, _ := strconv.Atoi(m[2])
			frac := 0.0
			if m[3] != "" {
				f, _ := strconv.ParseFloat("0."+m[3], 64)
				frac = f
			}
			at := time.Duration(min)*time.Minute + time.Duration(sec)*time.Second + time.Duration(frac*float64(time.Second))
			out = append(out, LyricLine{At: at, Text: words})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

func parsePlain(text string) []LyricLine {
	var out []LyricLine
	blank := true
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			if !blank {
				out = append(out, LyricLine{})
			}
			blank = true
			continue
		}
		out = append(out, LyricLine{Text: raw})
		blank = false
	}
	for len(out) > 0 && out[len(out)-1].Text == "" {
		out = out[:len(out)-1]
	}
	return out
}

func lyricsFromRecord(r *lrclibRecord) *Lyrics {
	switch {
	case r.Instrumental:
		return &Lyrics{Found: true, Instrumental: true}
	case strings.TrimSpace(r.SyncedLyrics) != "":
		if lines := parseLRC(r.SyncedLyrics); len(lines) > 0 {
			return &Lyrics{Found: true, Synced: true, Lines: lines}
		}
	}
	if lines := parsePlain(r.PlainLyrics); len(lines) > 0 {
		return &Lyrics{Found: true, Lines: lines}
	}
	return &Lyrics{}
}

func lrclibGet(path string, q url.Values, into any) (found bool, err error) {
	req, err := http.NewRequest("GET", lyricsBase+path+"?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "kindle-dash/2.0 (personal e-ink dashboard)")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return false, fmt.Errorf("lyrics service: %s", safeErr(err))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 404:
		return false, nil
	case resp.StatusCode != 200:
		return false, fmt.Errorf("lyrics service: HTTP %d", resp.StatusCode)
	}
	return true, json.NewDecoder(resp.Body).Decode(into)
}

// FetchLyrics looks the track up exactly, then without version decorations, then by search.
func FetchLyrics(title, artist, album string, dur time.Duration) (*Lyrics, error) {
	names := []string{title}
	if c := cleanTitle(title); c != "" && c != title {
		names = append(names, c)
	}
	for _, name := range names {
		q := url.Values{"track_name": {name}, "artist_name": {artist}, "album_name": {album}}
		if dur > 0 {
			q.Set("duration", strconv.Itoa(int(dur.Round(time.Second).Seconds())))
		}
		var rec lrclibRecord
		found, err := lrclibGet("/get", q, &rec)
		if err != nil {
			return nil, err
		}
		if found {
			if l := lyricsFromRecord(&rec); l.Found {
				return l, nil
			}
		}
	}
	var results []lrclibRecord
	q := url.Values{"track_name": {names[len(names)-1]}, "artist_name": {artist}}
	if _, err := lrclibGet("/search", q, &results); err != nil {
		return nil, err
	}
	var best *lrclibRecord
	for i := range results {
		r := &results[i]
		if dur > 0 && absDur(time.Duration(r.Duration*float64(time.Second))-dur) > 6*time.Second {
			continue
		}
		if best == nil || (r.SyncedLyrics != "" && best.SyncedLyrics == "") {
			best = r
		}
	}
	if best != nil {
		return lyricsFromRecord(best), nil
	}
	return &Lyrics{}, nil
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// lyricsKey identifies a track for caching.
func lyricsKey(p Player) string {
	return fmt.Sprintf("%s|%s|%s|%d", p.Title, p.Artist, p.Album, int(p.Dur.Seconds()))
}

// demoLyrics is made-up text, so the screen can be designed without touching real song lyrics.
func demoLyrics() *Lyrics {
	text := []string{
		"The kitchen light hums a quiet tune",
		"while the kettle practises its small hello",
		"Somewhere a window is open to June",
		"and the curtains forget which way to go",
		"",
		"We count the stairs in a sleepy order",
		"one for the morning and two for the rain",
		"The street lamps lean on the edge of the border",
		"between yesterday and the next refrain",
		"",
		"So leave the clock to its patient turning",
		"leave the map folded in the drawer",
		"There is nothing here that needs learning",
		"only the way the evening pours",
		"",
		"And when the last page of the day is closing",
		"we will keep the hallway lamp aglow",
		"No one is asking, no one is knowing",
		"how slowly an ordinary night can flow",
	}
	l := &Lyrics{Found: true, Synced: true}
	at := 6 * time.Second
	for _, t := range text {
		if t == "" {
			at += 3 * time.Second
			continue
		}
		l.Lines = append(l.Lines, LyricLine{At: at, Text: t})
		at += 7 * time.Second
	}
	return l
}
