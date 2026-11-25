// Command kdash-setup puts Kindle Dash on a Kindle that is plugged into this computer as a USB drive.
//
// It asks for what only you can provide (your own Spotify app's Client ID, the secret address of each
// Google calendar), runs the Spotify login in your browser, and copies the program, your settings and
// the login onto the Kindle. Your Spotify password is only ever typed on Spotify's own page.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // so any time zone name can be checked on any computer
)

var version = "dev"

type options struct {
	kindle, out, clientID, timezone string
	calendars                       []string
	remove, update, noSpotify       bool
	noCalendar, yes                 bool
}

type ui struct {
	in  *bufio.Reader
	out io.Writer
	yes bool // never ask: use flags and defaults
}

func (u *ui) say(format string, a ...any) { fmt.Fprintf(u.out, format+"\n", a...) }

// ask prints a question and returns the answer, or def when the answer is empty.
func (u *ui) ask(q, def string) string {
	if u.yes {
		return def
	}
	if def != "" {
		fmt.Fprintf(u.out, "%s [%s]: ", q, def)
	} else {
		fmt.Fprintf(u.out, "%s: ", q)
	}
	line, _ := u.in.ReadString('\n')
	if line = strings.TrimSpace(line); line != "" {
		return line
	}
	return def
}

func (u *ui) confirm(q string, def bool) bool {
	hint := "Y/n"
	if !def {
		hint = "y/N"
	}
	a := strings.ToLower(u.ask(q+" ("+hint+")", ""))
	switch {
	case a == "":
		return def
	case strings.HasPrefix(a, "y"):
		return true
	}
	return false
}

func main() {
	var o options
	flag.StringVar(&o.kindle, "kindle", "", "folder of the Kindle's USB drive (found automatically if left out)")
	flag.StringVar(&o.out, "out", "", "write the files into this folder instead of onto a Kindle")
	flag.BoolVar(&o.remove, "remove", false, "take Kindle Dash off the Kindle again")
	flag.BoolVar(&o.update, "update", false, "update the program only; keep the settings and logins already on the Kindle")
	flag.StringVar(&o.clientID, "client-id", "", "your Spotify app's Client ID")
	flag.BoolVar(&o.noSpotify, "no-spotify", false, "skip Spotify")
	flag.Func("calendar", "secret iCal address of a calendar to show (repeat for more)", func(s string) error {
		o.calendars = append(o.calendars, s)
		return nil
	})
	flag.BoolVar(&o.noCalendar, "no-calendar", false, "skip calendars")
	flag.StringVar(&o.timezone, "timezone", "", "time zone name such as Europe/Berlin (detected if left out)")
	flag.BoolVar(&o.yes, "yes", false, "do not ask anything: use the options given and sensible defaults")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("kdash-setup", version)
		return
	}
	u := &ui{in: bufio.NewReader(os.Stdin), out: os.Stdout, yes: o.yes}
	if err := run(o, u); err != nil {
		fmt.Fprintln(os.Stderr, "\nStopped:", err)
		pause(u)
		os.Exit(1)
	}
	pause(u)
}

// pause keeps a double-clicked window open on Windows until a key is pressed.
func pause(u *ui) {
	if u.yes {
		return
	}
	fmt.Fprint(u.out, "\nPress Enter to close this window.")
	_, _ = u.in.ReadString('\n')
}

var clientIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func run(o options, u *ui) error {
	u.say("Kindle Dash setup %s\n", version)
	root, isKindleDrive, err := chooseTarget(o, u)
	if err != nil {
		return err
	}
	if o.remove {
		if err := removeInstall(root); err != nil {
			return err
		}
		u.say("Removed Kindle Dash from %s. Eject the Kindle safely before unplugging it.", root)
		return nil
	}
	if isKindleDrive && !hasJailbreak(root) {
		u.say("\nNote: I do not see the jailbreak's files on this Kindle (no \"libkh\" folder). Kindle Dash only")
		u.say("runs on a jailbroken Kindle - see the README for the short list of what you need first.")
		if !o.yes && !u.confirm("Install anyway?", false) {
			return errors.New("cancelled")
		}
	}

	cfg, existing := loadSettings(root)
	update := o.update
	if update && !existing {
		u.say("\nNothing is set up on this Kindle yet, so I will ask for your settings first.")
		update = false
	}
	if existing && !update && !o.yes {
		u.say("\nKindle Dash is already set up here (%d calendars%s).", cfg.calendarCount(),
			map[bool]string{true: ", Spotify", false: ""}[cfg.clientID() != ""])
		update = u.confirm("Just update the program and keep your settings and logins?", true)
	}

	var tok *tokenFile
	if !update {
		if tok, err = configure(o, u, cfg); err != nil {
			return err
		}
	}

	u.say("\nCopying files ...")
	if err := installProgram(root); err != nil {
		return err
	}
	if !update {
		if err := saveSettings(root, cfg, tok); err != nil {
			return err
		}
	}
	u.say("\nDone. Files are in %s", filepath.Join(root, installDir))
	if isKindleDrive {
		u.say("\nNext:")
		u.say("  1. Eject the Kindle safely on this computer, then unplug it.")
		u.say("  2. On the Kindle home screen, open \"Kindle Dash Diagnose\" once. It checks Wi-Fi, Spotify and your")
		u.say("     calendars and shows the result on screen.")
		u.say("  3. Then open \"Kindle Dash\" any time. Plugging in the USB cable closes it again.")
	} else {
		u.say("\nCopy the \"%s\" folder to the top level of the Kindle's drive and the two .sh files from \"documents\"", installDir)
		u.say("into the Kindle's \"documents\" folder, then eject the Kindle.")
	}
	return nil
}

// chooseTarget finds the Kindle (or the folder to write to).
func chooseTarget(o options, u *ui) (root string, kindle bool, err error) {
	switch {
	case o.out != "":
		if err := os.MkdirAll(filepath.Join(o.out, "documents"), 0o755); err != nil {
			return "", false, err
		}
		return o.out, false, nil
	case o.kindle != "":
		if !isKindle(o.kindle) {
			return "", false, fmt.Errorf("%s does not look like a Kindle drive (it has no documents and system folders)", o.kindle)
		}
		return o.kindle, true, nil
	}
	for {
		found := findKindles()
		switch len(found) {
		case 1:
			u.say("Found your Kindle at %s", found[0])
			return found[0], true, nil
		case 0:
		default:
			u.say("Found several drives that look like Kindles:")
			for i, f := range found {
				u.say("  %d. %s", i+1, f)
			}
			if u.yes {
				return "", false, errors.New("more than one Kindle found; pass -kindle FOLDER")
			}
			var n int
			fmt.Sscan(u.ask("Which one", "1"), &n)
			if n >= 1 && n <= len(found) {
				return found[n-1], true, nil
			}
			continue
		}
		if u.yes {
			return "", false, errors.New("no Kindle found; plug it in or pass -kindle FOLDER (or -out FOLDER)")
		}
		u.say("I could not find a Kindle. Plug it into this computer with its USB cable and wait until it shows up")
		u.say("as a drive (on the Kindle itself you may need to tap nothing: it switches to file transfer by itself).")
		a := u.ask("Press Enter to look again, type the drive's folder, or type o to just create the files in a folder", "")
		switch {
		case a == "":
		case strings.EqualFold(a, "o"):
			dir := u.ask("Folder to create", "kindle-dash-files")
			return chooseTarget(options{out: dir}, u)
		case isKindle(a):
			return a, true, nil
		default:
			u.say("%s does not look like a Kindle drive.", a)
		}
	}
}

// configure asks for Spotify, calendars and the time zone and fills in cfg.
func configure(o options, u *ui, cfg settings) (*tokenFile, error) {
	var tok *tokenFile

	// --- Spotify
	if o.noSpotify {
		delete(cfg, "spotify_client_id")
	} else {
		id := o.clientID
		if id == "" && !u.yes {
			u.say("\n== Spotify (for the music screens) ==")
			u.say("Spotify wants you to register your own free \"app\" so it can let the Kindle talk to your account.")
			u.say("It takes two minutes:")
			u.say("  1. Open https://developer.spotify.com/dashboard and log in.")
			u.say("  2. Click \"Create app\". Any name and description will do.")
			u.say("     Redirect URI: %s   (type it exactly, then click Add)", redirectURI)
			u.say("     Tick \"Web API\", accept the terms, Save.")
			u.say("  3. Open the app's Settings and copy the \"Client ID\".")
			u.say("Controlling playback needs Spotify Premium. The Client ID is not a secret.")
			id = u.ask("Client ID (just press Enter to skip Spotify)", cfg.clientID())
		}
		switch {
		case id == "":
			u.say("Skipping Spotify. Run this program again any time to add it.")
			delete(cfg, "spotify_client_id")
		case !clientIDPattern.MatchString(id):
			return nil, errors.New("a Spotify Client ID is 32 letters and digits; check what you copied")
		default:
			cfg["spotify_client_id"] = id
			u.say("\nOpening Spotify in your browser. Log in there and click Agree.")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			var err error
			tok, err = spotifyLogin(ctx, id, redirectHost, func(url string) {
				u.say("If the page does not open by itself, copy this address into your browser:\n\n%s\n", url)
				openBrowser(url)
			})
			if err != nil {
				return nil, fmt.Errorf("Spotify login failed: %w", err)
			}
			u.say("Spotify connected. The login is stored only on the Kindle.")
		}
	}

	// --- calendars
	specs := []map[string]string{}
	if !o.noCalendar {
		addrs := o.calendars
		if len(addrs) == 0 && !u.yes {
			u.say("\n== Google Calendar (for the calendar screens) ==")
			u.say("For each calendar you want to show, in Google Calendar on a computer: Settings -> click the")
			u.say("calendar's name on the left -> \"Integrate calendar\" -> copy \"Secret address in iCal format\".")
			u.say("Keep that address private: anyone who has it can read the calendar. It is saved only on the Kindle,")
			u.say("and this program never prints it back.")
			u.say("Paste one address per line. Press Enter on an empty line when you are done.")
			for {
				line := u.ask("Address", "")
				if line == "" {
					break
				}
				addrs = append(addrs, strings.Fields(line)...)
			}
		}
		for i, raw := range addrs {
			addr := normalizeCalendarURL(raw)
			events, name, err := checkCalendar(addr)
			if err != nil {
				if u.yes {
					return nil, fmt.Errorf("calendar %d: %w", i+1, err)
				}
				u.say("  Calendar %d: %v", i+1, err)
				if !u.confirm("  Add it anyway?", false) {
					continue
				}
			} else {
				u.say("  Calendar %d: found %d events", i+1, events)
			}
			if name == "" {
				name = fmt.Sprintf("Calendar %d", len(specs)+1)
			}
			name = u.ask("  Name to show on the Kindle", name)
			specs = append(specs, map[string]string{"name": name, "url": addr})
		}
		if len(specs) == 0 {
			u.say("No calendars added. The calendar screens will be empty until you run this program again.")
		}
	}
	if o.noCalendar {
		delete(cfg, "calendars")
	} else {
		cfg["calendars"] = specs
	}

	// --- time zone and defaults
	tz := o.timezone
	if tz == "" {
		def := detectTimezone()
		if def == "" {
			def = "UTC"
		}
		tz = u.ask("\nYour time zone, such as Europe/Berlin or America/New_York", def)
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return nil, fmt.Errorf("unknown time zone %q (use a name like Europe/Berlin)", tz)
	}
	cfg["timezone"] = tz
	for k, v := range map[string]any{"clock24": true, "week_starts_monday": true, "calendar_days": 7, "calendar_lookahead": 60} {
		if _, have := cfg[k]; !have {
			cfg[k] = v
		}
	}
	return tok, nil
}
