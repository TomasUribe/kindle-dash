package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strings"
	"syscall"
	"unsafe"
)

// InputEvent is a finished gesture, in panel (portrait) pixels.
type InputEvent struct {
	Kind   string // tap, swipe, power
	X, Y   int
	DX, DY int // for swipes: how far the finger travelled
}

const (
	evSyn, evKey, evAbs     = 0, 1, 3
	absMTX, absMTY, absMTID = 53, 54, 57
	btnTouch, keyPower      = 330, 116
)

var (
	touchName = regexp.MustCompile(`(?i)touch|_mt|cyttsp|zforce|elan|ft5|goodix|hid-over`)
	powerName = regexp.MustCompile(`(?i)power|gpio-keys|pwr|pmic|keypad|button`)
	tvSize    = int(unsafe.Sizeof(syscall.Timeval{}))
	evSize    = tvSize + 8 // timeval + type(2) + code(2) + value(4)
)

type inputDev struct{ name, event string }

// listInputDevices parses /proc/bus/input/devices into name and event node pairs.
func listInputDevices() []inputDev {
	f, err := os.Open("/proc/bus/input/devices")
	if err != nil {
		return nil
	}
	defer f.Close()
	var devs []inputDev
	var name string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "N: Name="):
			name = strings.Trim(strings.TrimPrefix(line, "N: Name="), `"`)
		case strings.HasPrefix(line, "H: Handlers="):
			for _, h := range strings.Fields(strings.TrimPrefix(line, "H: Handlers=")) {
				if strings.HasPrefix(h, "event") {
					devs = append(devs, inputDev{name, "/dev/input/" + h})
				}
			}
		}
	}
	return devs
}

func findInput(re *regexp.Regexp, exclude *regexp.Regexp) []inputDev {
	var out []inputDev
	for _, d := range listInputDevices() {
		if re.MatchString(d.name) && (exclude == nil || !exclude.MatchString(d.name)) {
			out = append(out, d)
		}
	}
	return out
}

func ioctlAbs(f *os.File, axis uintptr) (min, max int32, ok bool) {
	var info [6]int32 // value, min, max, fuzz, flat, resolution
	req := uintptr(2<<30 | 24<<16 | 'E'<<8 | (0x40 + axis))
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&info[0]))); e != 0 {
		return 0, 0, false
	}
	return info[1], info[2], info[2] > info[1]
}

func grab(f *os.File) {
	req := uintptr(1<<30 | 4<<16 | 'E'<<8 | 0x90)
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, 1)
}

// WatchTouch turns raw touch events from one device into taps and swipes.
func WatchTouch(path string, out chan<- InputEvent, logf func(string, ...any)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	grab(f) // the stock UI must not see these touches too
	minX, maxX, okX := ioctlAbs(f, absMTX)
	minY, maxY, okY := ioctlAbs(f, absMTY)
	if !okX {
		minX, maxX = 0, ScreenW-1
	}
	if !okY {
		minY, maxY = 0, ScreenH-1
	}
	logf("touch %s: x %d..%d y %d..%d (event size %d)", path, minX, maxX, minY, maxY, evSize)
	scale := func(v, lo, hi int32, size int) int {
		if hi <= lo {
			return int(v)
		}
		return int(float64(v-lo) / float64(hi-lo) * float64(size-1))
	}

	go func() {
		defer f.Close()
		buf := make([]byte, evSize)
		var rawX, rawY int32
		var haveX, haveY, down, started bool
		var sx, sy, lx, ly int
		lift := func() {
			if started {
				dx, dy := lx-sx, ly-sy
				if math.Hypot(float64(dx), float64(dy)) < 70 {
					out <- InputEvent{Kind: "tap", X: lx, Y: ly}
				} else {
					out <- InputEvent{Kind: "swipe", X: sx, Y: sy, DX: dx, DY: dy}
				}
			}
			down, started, haveX, haveY = false, false, false, false
		}
		for {
			if _, err := io.ReadFull(f, buf); err != nil {
				logf("touch read stopped: %v", err)
				return
			}
			typ := binary.LittleEndian.Uint16(buf[tvSize:])
			code := binary.LittleEndian.Uint16(buf[tvSize+2:])
			val := int32(binary.LittleEndian.Uint32(buf[tvSize+4:]))
			switch {
			case typ == evAbs && code == absMTX:
				rawX, haveX = val, true
				down = true
			case typ == evAbs && code == absMTY:
				rawY, haveY = val, true
				down = true
			case typ == evAbs && code == absMTID:
				if val == -1 {
					lift()
				} else {
					down = true
				}
			case typ == evKey && code == btnTouch:
				if val == 0 {
					lift()
				} else {
					down = true
				}
			case typ == evSyn && code == 0:
				if down && haveX && haveY {
					lx, ly = scale(rawX, minX, maxX, ScreenW), scale(rawY, minY, maxY, ScreenH)
					if !started {
						sx, sy, started = lx, ly, true
					}
				}
			}
		}
	}()
	return nil
}

// WatchPower reports presses of the power button.
func WatchPower(path string, out chan<- InputEvent, logf func(string, ...any)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	logf("power button watcher on %s", path)
	go func() {
		defer f.Close()
		buf := make([]byte, evSize)
		for {
			if _, err := io.ReadFull(f, buf); err != nil {
				return
			}
			typ := binary.LittleEndian.Uint16(buf[tvSize:])
			code := binary.LittleEndian.Uint16(buf[tvSize+2:])
			val := int32(binary.LittleEndian.Uint32(buf[tvSize+4:]))
			if typ == evKey && code == keyPower && val == 1 {
				out <- InputEvent{Kind: "power"}
			}
		}
	}()
	return nil
}

// StartInput finds the touchscreen and power button and starts watching them.
func StartInput(touchOverride string, out chan<- InputEvent, logf func(string, ...any)) error {
	for _, d := range listInputDevices() {
		logf("input device: %q -> %s", d.name, d.event)
	}
	touch := touchOverride
	if touch == "" {
		if found := findInput(touchName, nil); len(found) > 0 {
			touch = found[0].event
		}
	}
	if touch == "" {
		return fmt.Errorf("no touchscreen found in /proc/bus/input/devices")
	}
	if err := WatchTouch(touch, out, logf); err != nil {
		return err
	}
	for _, d := range findInput(powerName, touchName) {
		if err := WatchPower(d.event, out, logf); err != nil {
			logf("power watcher %s: %v", d.event, err)
		}
	}
	return nil
}
