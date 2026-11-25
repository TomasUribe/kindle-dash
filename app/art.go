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

// ArtCache downloads the current album cover once and hands it out at any size, in gray.
type ArtCache struct {
	mu      sync.Mutex
	url     string
	src     image.Image
	sized   map[int]*image.Gray
	loading string
	failed  string
}

func NewArtCache() *ArtCache { return &ArtCache{sized: map[int]*image.Gray{}} }

// Request starts a download when the cover changed; done runs after a new cover is ready.
func (a *ArtCache) Request(url string, done func()) {
	if url == "" {
		return
	}
	a.mu.Lock()
	if a.url == url || a.loading == url || a.failed == url {
		a.mu.Unlock()
		return
	}
	a.loading = url
	a.mu.Unlock()
	go func() {
		img, err := loadArt(url)
		a.mu.Lock()
		a.loading = ""
		if err != nil {
			a.failed = url
		} else {
			a.url, a.src, a.sized = url, img, map[int]*image.Gray{}
		}
		a.mu.Unlock()
		if err == nil && done != nil {
			done()
		}
	}()
}

// Get returns the cover for url at size x size, or nil when it is not loaded.
func (a *ArtCache) Get(url string, size int) *image.Gray {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.url != url || a.src == nil {
		return nil
	}
	if g, ok := a.sized[size]; ok {
		return g
	}
	g := ToGray(a.src, size, size)
	a.sized[size] = g
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
