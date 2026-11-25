package main

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ArtCache downloads album covers once and hands them out at any size, in gray. It keeps a few, so the
// next song's cover can be warmed while the current one is still on screen.
type ArtCache struct {
	mu      sync.Mutex
	entries map[string]*artEntry
	order   []string             // oldest first, for eviction
	loading map[string][]func()  // url -> who to tell when it is ready
	failed  map[string]time.Time // url -> when it failed; tried again after artRetry
}

type artEntry struct {
	src   image.Image
	sized map[int]*image.Gray
}

const (
	artKeep  = 3
	artRetry = time.Minute
)

// fetchArt is how covers are downloaded; tests replace it.
var fetchArt = loadArt

func NewArtCache() *ArtCache {
	return &ArtCache{entries: map[string]*artEntry{}, loading: map[string][]func(){}, failed: map[string]time.Time{}}
}

// Request starts a download unless the cover is already here or on its way; done (if any) runs once the
// cover is ready, including when it was already downloading for someone else.
func (a *ArtCache) Request(url string, done func()) {
	if url == "" {
		return
	}
	a.mu.Lock()
	if _, ok := a.entries[url]; ok {
		a.mu.Unlock()
		return
	}
	if at, bad := a.failed[url]; bad && time.Since(at) < artRetry {
		a.mu.Unlock()
		return
	}
	if waiters, busy := a.loading[url]; busy {
		if done != nil {
			a.loading[url] = append(waiters, done)
		}
		a.mu.Unlock()
		return
	}
	var waiters []func()
	if done != nil {
		waiters = []func(){done}
	}
	a.loading[url] = waiters
	a.mu.Unlock()
	go func() {
		img, err := fetchArt(url)
		a.mu.Lock()
		waiters := a.loading[url]
		delete(a.loading, url)
		if err != nil {
			a.failed[url] = time.Now()
		} else {
			delete(a.failed, url)
			a.entries[url] = &artEntry{src: img, sized: map[int]*image.Gray{}}
			a.order = append(a.order, url)
			for len(a.order) > artKeep {
				delete(a.entries, a.order[0])
				a.order = a.order[1:]
			}
		}
		a.mu.Unlock()
		if err == nil {
			for _, f := range waiters {
				f()
			}
		}
	}()
}

// Get returns the cover for url at size x size, or nil when it is not loaded.
func (a *ArtCache) Get(url string, size int) *image.Gray {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.entries[url]
	if e == nil {
		return nil
	}
	if g, ok := e.sized[size]; ok {
		return g
	}
	g := ToGray(e.src, size, size)
	e.sized[size] = g
	return g
}

func loadArt(url string) (image.Image, error) {
	if idx, ok := strings.CutPrefix(url, "demo:"); ok {
		if idx == "prism" {
			// KDASH_DEMO_COVER=file.png swaps the drawn prism for any picture, for your own screenshots
			if f := os.Getenv("KDASH_DEMO_COVER"); f != "" {
				if img, err := decodeFile(f); err == nil {
					return img, nil
				}
			}
			return prismCover(), nil
		}
		var i int
		fmt.Sscan(idx, &i)
		return demoCover(i), nil
	}
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 8<<20))
	return img, err
}

// demoCover draws a deterministic abstract cover, so the design can be judged without Spotify.
func demoCover(i int) image.Image {
	const n = 640
	g := image.NewGray(image.Rect(0, 0, n, n))
	cx, cy := float64(n)*(0.35+0.1*float64(i%4)), float64(n)*(0.4+0.08*float64((i*3)%4))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			v := 0.5 + 0.5*math.Sin(d/(18+float64(i)*5))
			v = v*0.8 + 0.2*float64(x)/float64(n)
			g.SetGray(x, y, color.Gray{Y: uint8(255 * math.Max(0, math.Min(1, v)))})
		}
	}
	return g
}

func decodeFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}
