package main

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

const (
	powerd       = "com.lab126.powerd"
	lightDefault = 24 // flIntensity and currentAmberLevel both run 0..24 on current Kindles
)

// LightState is what the Settings screen shows.
type LightState struct {
	Available       bool // the front light can be read and set
	Level, Max      int
	HasWarmth       bool // an adjustable warm light exists
	Warmth, WarmMax int
}

// FrontLight reads and sets the front light through the Kindle's power service (lipc).
// Off the Kindle (on a computer) it pretends, so the screen can be tried out.
type FrontLight struct {
	mu   sync.Mutex
	fake bool
	st   LightState
}

func NewFrontLight() *FrontLight {
	if _, err := exec.LookPath("lipc-get-prop"); err != nil {
		return &FrontLight{fake: true, st: LightState{Available: true, Level: 12, Max: lightDefault,
			HasWarmth: true, Warmth: 8, WarmMax: lightDefault}}
	}
	return &FrontLight{}
}

func lipcGet(prop string) (int, error) {
	out, err := exec.Command("lipc-get-prop", powerd, prop).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// lipcSet writes an integer property. Plain form first (it works for flIntensity), then -i.
func lipcSet(prop string, v int) error {
	val := strconv.Itoa(v)
	if err := exec.Command("lipc-set-prop", powerd, prop, val).Run(); err == nil {
		return nil
	}
	return exec.Command("lipc-set-prop", "-i", powerd, prop, val).Run()
}

func clampInt(v, lo, hi int) int { return max(lo, min(hi, v)) }

// Read returns the current state; warmth is only offered when the Kindle reports it.
func (l *FrontLight) Read() LightState {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fake {
		return l.st
	}
	st := LightState{Max: lightDefault, WarmMax: lightDefault}
	if v, err := lipcGet("flIntensity"); err == nil {
		st.Available, st.Level = true, v
	}
	if v, err := lipcGet("flMaxIntensity"); err == nil && v >= 8 {
		st.Max = min(v, lightDefault)
	}
	if v, err := lipcGet("currentAmberLevel"); err == nil {
		st.HasWarmth, st.Warmth = true, v
	}
	st.Level = clampInt(st.Level, 0, st.Max)
	st.Warmth = clampInt(st.Warmth, 0, st.WarmMax)
	return st
}

func (l *FrontLight) SetLevel(v int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fake {
		l.st.Level = clampInt(v, 0, l.st.Max)
		return nil
	}
	return lipcSet("flIntensity", clampInt(v, 0, lightDefault))
}

func (l *FrontLight) SetWarmth(v int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fake {
		l.st.Warmth = clampInt(v, 0, l.st.WarmMax)
		return nil
	}
	return lipcSet("currentAmberLevel", clampInt(v, 0, lightDefault))
}
