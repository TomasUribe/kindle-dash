package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	_ "time/tzdata" // the Kindle has no reliable zoneinfo, so ship the database inside the binary
)

// CalendarSpec is one iCal feed (a Google "secret address in iCal format").
type CalendarSpec struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Config is read from config.json next to the program (written by tools/make-device-config.py).
type Config struct {
	Calendars          []CalendarSpec `json:"calendars"`
	CalendarDays       int            `json:"calendar_days"`      // days always listed
	CalendarLookahead  int            `json:"calendar_lookahead"` // how far to look on a quiet week
	CalendarTTLSeconds int            `json:"calendar_ttl_seconds"`
	SpotifyClientID    string         `json:"spotify_client_id"`
	SpotifyTokenFile   string         `json:"spotify_token_file"`
	Timezone           string         `json:"timezone"`
	Clock24            *bool          `json:"clock24"`
	WeekStartsMonday   *bool          `json:"week_starts_monday"`
	PollSeconds        int            `json:"poll_seconds"`
	ProgressSeconds    int            `json:"progress_seconds"`   // progress bar moves in steps this long
	FullRefreshEvery   int            `json:"full_refresh_every"` // partial updates before a clean flash
	FbinkFullArgs      []string       `json:"fbink_full_args"`    // default: -c -f
	FbinkPartialArgs   []string       `json:"fbink_partial_args"` // default: none (try -W GL16)
	LyricsEnabled      *bool          `json:"lyrics"`
	LyricsLeadSeconds  float64        `json:"lyrics_lead_seconds"`     // show a line this long before it is sung
	NextTrackLead      float64        `json:"next_track_lead_seconds"` // show the next song's cover and title this long before it starts; negative: off
	StateFile          string         `json:"state_file"`              // remembers view and orientation

	Loc *time.Location `json:"-"`
	Dir string         `json:"-"`
}

func (c *Config) Use24h() bool      { return c.Clock24 == nil || *c.Clock24 }
func (c *Config) UseLyrics() bool   { return c.LyricsEnabled == nil || *c.LyricsEnabled }
func (c *Config) MondayFirst() bool { return c.WeekStartsMonday == nil || *c.WeekStartsMonday }

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Dir = filepath.Dir(path)
	c.applyDefaults()
	return &c, c.resolveZone()
}

// DemoConfig is used with -demo: fake data, no accounts, any time zone.
func DemoConfig() *Config {
	c := &Config{Timezone: "Local", Dir: filepath.Join(os.TempDir(), "kdash-demo")}
	_ = os.MkdirAll(c.Dir, 0o755)
	c.applyDefaults()
	_ = c.resolveZone()
	return c
}

func (c *Config) applyDefaults() {
	if c.CalendarDays <= 0 {
		c.CalendarDays = 7
	}
	if c.CalendarLookahead < c.CalendarDays {
		c.CalendarLookahead = 60
	}
	if c.CalendarTTLSeconds <= 0 {
		c.CalendarTTLSeconds = 300
	}
	if c.PollSeconds <= 0 {
		c.PollSeconds = 10 // e-ink is slow: asking more often only costs battery
	}
	if c.ProgressSeconds <= 0 {
		c.ProgressSeconds = 15
	}
	if c.LyricsLeadSeconds <= 0 {
		c.LyricsLeadSeconds = 2.0 // the panel needs a moment to redraw, so the highlight runs early
	}
	if c.NextTrackLead == 0 {
		c.NextTrackLead = 3 // the screen needs a moment, so the next cover goes up just before the song starts
	}
	if c.FullRefreshEvery <= 0 {
		c.FullRefreshEvery = 40
	}
	if len(c.FbinkFullArgs) == 0 {
		c.FbinkFullArgs = []string{"-c", "-f"}
	}
	if c.Timezone == "" {
		c.Timezone = "UTC"
	}
	c.SpotifyTokenFile = c.inDir(c.SpotifyTokenFile, "spotify_token.json")
	c.StateFile = c.inDir(c.StateFile, "state.json")
}

func (c *Config) inDir(p, def string) string {
	if p == "" {
		p = def
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.Dir, p)
}

func (c *Config) resolveZone() error {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return fmt.Errorf("timezone %q: %w", c.Timezone, err)
	}
	c.Loc = loc
	return nil
}

// nowIn is the current time in the configured zone.
func (c *Config) nowIn() time.Time { return time.Now().In(c.Loc) }
