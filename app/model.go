package main

import "time"

// Event is one calendar entry after repeats have been expanded.
type Event struct {
	Title, Location, Cal string
	Start, End           time.Time
	AllDay               bool
}

// Row is an event as listed under one particular day.
type Row struct {
	Title, Location, Cal, When string
	Start, End                 time.Time
	AllDay                     bool
}

// Day is one heading plus its rows in the agenda.
type Day struct {
	Label string    // Today, Tomorrow, Saturday
	Sub   string    // Sat 05 Dec
	Date  time.Time // local midnight
	Rows  []Row
}

// Agenda is what the calendar views draw.
type Agenda struct {
	Days   []Day
	Events []Event // flat list, used for the month grid dots
	Demo   bool
	Err    string
}

// Player is what the music views draw.
type Player struct {
	Demo          bool
	Active        bool
	Playing       bool
	Title, Artist string
	Album, Device string
	ArtURL        string
	TrackURI      string
	Progress, Dur time.Duration
	Volume        int // -1 when the device has no remote volume
	Hint          string
	Err           string
	FetchedAt     time.Time
}

// Expected is the progress now, assuming playback kept going since FetchedAt.
func (p Player) Expected(now time.Time) time.Duration {
	d := p.Progress
	if p.Playing && !p.FetchedAt.IsZero() {
		d += now.Sub(p.FetchedAt)
	}
	if p.Dur > 0 && d > p.Dur {
		d = p.Dur
	}
	return d
}
