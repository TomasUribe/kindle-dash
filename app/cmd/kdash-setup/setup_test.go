package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// The worked example from RFC 7636, appendix B.
func TestPKCEChallenge(t *testing.T) {
	got := pkceChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"; got != want {
		t.Errorf("challenge %q, want %q", got, want)
	}
	if v := randomURLSafe(64); len(v) < 43 || len(v) > 128 || v == randomURLSafe(64) {
		t.Errorf("verifier should be 43-128 random characters: %q", v)
	}
}

func TestAuthorizeURLAsksForOnlyWhatIsUsed(t *testing.T) {
	u, err := url.Parse(authorizeURL("abc123", "chal", "st"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "abc123" || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != redirectURI || q.Get("state") != "st" {
		t.Errorf("query: %v", q)
	}
	scopes := strings.Fields(q.Get("scope"))
	if len(scopes) != len(spotifyScopes) {
		t.Errorf("scopes: %v", scopes)
	}
	for _, s := range scopes {
		if strings.Contains(s, "email") || strings.Contains(s, "private") && !strings.HasPrefix(s, "playlist-read") {
			t.Errorf("unexpectedly broad scope %q", s)
		}
	}
}

// A pretend Spotify: the "browser" visits the callback with the right state, then the code is exchanged.
func TestSpotifyLoginRoundTrip(t *testing.T) {
	var gotForm url.Values
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		fmt.Fprint(w, `{"access_token":"AT","refresh_token":"RT","expires_in":3600,"scope":"user-read-playback-state"}`)
	}))
	defer fake.Close()
	old := spotifyTokenURL
	spotifyTokenURL = fake.URL
	defer func() { spotifyTokenURL = old }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ln := freePort(t)
	done := make(chan *tokenFile, 1)
	errc := make(chan error, 1)
	go func() {
		tk, e := spotifyLogin(ctx, "client1", ln, func(authURL string) {
			go func() {
				au, _ := url.Parse(authURL)
				st := au.Query().Get("state")
				time.Sleep(100 * time.Millisecond)
				resp, e := http.Get("http://" + ln + "/callback?code=THECODE&state=" + url.QueryEscape(st))
				if e == nil {
					resp.Body.Close()
				}
			}()
		})
		done <- tk
		errc <- e
	}()
	select {
	case tk := <-done:
		if e := <-errc; e != nil || tk == nil {
			t.Fatalf("login: %v", e)
		}
		if tk.AccessToken != "AT" || tk.RefreshToken != "RT" || tk.ExpiresAt < float64(time.Now().Unix()+3000) {
			t.Errorf("token: %+v", tk)
		}
	case <-ctx.Done():
		t.Fatal("login never finished")
	}
	if gotForm.Get("code") != "THECODE" || gotForm.Get("client_id") != "client1" || gotForm.Get("grant_type") != "authorization_code" ||
		gotForm.Get("code_verifier") == "" || gotForm.Get("client_secret") != "" {
		t.Errorf("token request: %v", gotForm)
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.Listener.Addr().String()
	srv.Close()
	return addr
}

func TestSpotifyLoginRejectsWrongState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addr := freePort(t)
	_, err := spotifyLogin(ctx, "c", addr, func(string) {
		go func() {
			time.Sleep(100 * time.Millisecond)
			if resp, e := http.Get("http://" + addr + "/callback?code=X&state=forged"); e == nil {
				resp.Body.Close()
			}
		}()
	})
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("a callback with the wrong state must be refused, got %v", err)
	}
}

func TestCheckCalendarNeverEchoesTheAddress(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good/secret-token-123/basic.ics":
			fmt.Fprint(w, "BEGIN:VCALENDAR\r\nX-WR-CALNAME:Team\r\nBEGIN:VEVENT\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
		case "/html/secret-token-123":
			fmt.Fprint(w, "<html>sign in</html>")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	// the test server's certificate is not trusted by the default client
	http.DefaultTransport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig
	defer func() { http.DefaultTransport.(*http.Transport).TLSClientConfig = nil }()
	oldClient := checkClient
	checkClient = srv.Client()
	defer func() { checkClient = oldClient }()

	n, name, err := checkCalendar(srv.URL + "/good/secret-token-123/basic.ics")
	if err != nil || n != 2 || name != "Team" {
		t.Fatalf("good calendar: %d %q %v", n, name, err)
	}
	for _, bad := range []string{srv.URL + "/html/secret-token-123", srv.URL + "/missing/secret-token-123", "http://insecure.example/secret-token-123", "not a url"} {
		if _, _, err := checkCalendar(bad); err == nil {
			t.Errorf("%q should fail", bad)
		} else if strings.Contains(err.Error(), "secret-token-123") {
			t.Errorf("error leaks the address: %v", err)
		}
	}
	// a server that is not there: the transport error mentions the URL unless stripped
	srv.Close()
	if _, _, err := checkCalendar(srv.URL + "/good/secret-token-123/basic.ics"); err == nil || strings.Contains(err.Error(), "secret-token-123") {
		t.Errorf("connection errors must not leak the address either: %v", err)
	}
}

func TestNormalizeCalendarURL(t *testing.T) {
	for in, want := range map[string]string{
		"webcal://example.com/a.ics":   "https://example.com/a.ics",
		`  "https://example.com/a.ics"`: "https://example.com/a.ics",
		"<https://example.com/a.ics>":  "https://example.com/a.ics",
	} {
		if got := normalizeCalendarURL(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestScriptletIconIsEmbedded(t *testing.T) {
	script := []byte("#!/bin/sh\n# Name: X\n# Icon: /somewhere/icon.png\n# DontUseFBInk\n\nrun\n")
	got := string(scriptletWithIcon(script, []byte("PNGDATA")))
	if !strings.Contains(got, "# Icon: data:image/png;base64,UE5HREFUQQ==\n") || !strings.Contains(got, "# Name: X\n") || !strings.HasSuffix(got, "run\n") {
		t.Errorf("scriptlet: %q", got)
	}
	if string(scriptletWithIcon(script, nil)) != string(script) {
		t.Error("without an icon the script is untouched")
	}
}

func fakePayload(t *testing.T) {
	t.Helper()
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write([]byte("ELF-PRETEND"))
	w.Close()
	old := payloadFS
	payloadFS = fstest.MapFS{
		"payload/kdash.gz":       {Data: gz.Bytes()},
		"payload/run.sh":         {Data: []byte("#!/bin/sh\necho run\n")},
		"payload/cacert.pem":     {Data: []byte("CERTS")},
		"payload/icon.png":       {Data: []byte("PNG")},
		"payload/" + entryName:   {Data: []byte("#!/bin/sh\n# Name: Kindle Dash\n# Icon: x\nrun\n")},
		"payload/" + diagName:    {Data: []byte("#!/bin/sh\n# Name: Diagnose\n# Icon: x\ndiag\n")},
	}
	t.Cleanup(func() { payloadFS = old })
}

func fakeKindle(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"documents", "system", "libkh"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestInstallConfigureAndRemove(t *testing.T) {
	fakePayload(t)
	root := fakeKindle(t)
	if !isKindle(root) || !hasJailbreak(root) || isKindle(t.TempDir()) {
		t.Fatal("drive detection")
	}

	var out bytes.Buffer
	u := &ui{in: nil, out: &out, yes: true}
	o := options{kindle: root, clientID: "", noSpotify: true, calendars: nil, noCalendar: true, timezone: "Europe/Berlin", yes: true}
	if err := run(o, u); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	dir := filepath.Join(root, installDir)
	for name, want := range map[string]string{"kdash": "ELF-PRETEND", "cacert.pem": "CERTS", "icon.png": "PNG"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != want {
			t.Errorf("%s: %q %v", name, b, err)
		}
	}
	entry, _ := os.ReadFile(filepath.Join(root, "documents", entryName))
	if !strings.Contains(string(entry), "# Icon: data:image/png;base64,") {
		t.Errorf("library entry has no embedded icon: %s", entry)
	}
	var cfg map[string]any
	raw, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg["timezone"] != "Europe/Berlin" || cfg["clock24"] != true {
		t.Errorf("config: %s %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "spotify_token.json")); err == nil {
		t.Error("no login was made, so no token file should exist")
	}

	// settings the person tuned by hand survive running setup again with new answers
	cfg["poll_seconds"] = 20
	b, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600)
	o2 := options{kindle: root, noSpotify: true, noCalendar: true, timezone: "Asia/Tokyo", yes: true}
	if err := run(o2, u); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "config.json"))
	cfg = nil
	json.Unmarshal(raw, &cfg)
	if cfg["timezone"] != "Asia/Tokyo" || cfg["poll_seconds"] != float64(20) {
		t.Errorf("re-run should change what was asked and keep the rest: %s", raw)
	}

	// update only: program refreshed, settings untouched
	os.WriteFile(filepath.Join(dir, "kdash"), []byte("OLD"), 0o755)
	if err := run(options{kindle: root, update: true, yes: true}, u); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "kdash")); string(b) != "ELF-PRETEND" {
		t.Error("update should replace the program")
	}
	raw2, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if string(raw2) != string(raw) {
		t.Error("update must not touch config.json")
	}

	if err := run(options{kindle: root, remove: true, yes: true}, u); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("remove should delete the folder")
	}
	if _, err := os.Stat(filepath.Join(root, "documents", entryName)); err == nil {
		t.Error("remove should delete the library entry")
	}
	if !isKindle(root) {
		t.Error("remove must leave the rest of the drive alone")
	}
}

func TestRefusesAFolderThatIsNotAKindle(t *testing.T) {
	err := run(options{kindle: t.TempDir(), yes: true}, &ui{out: &bytes.Buffer{}, yes: true})
	if err == nil || !strings.Contains(err.Error(), "does not look like a Kindle") {
		t.Errorf("got %v", err)
	}
}

func TestBadInputIsRejected(t *testing.T) {
	fakePayload(t)
	root := fakeKindle(t)
	u := &ui{out: &bytes.Buffer{}, yes: true}
	if err := run(options{kindle: root, clientID: "not-a-client-id", timezone: "UTC", yes: true}, u); err == nil {
		t.Error("a malformed client id should be rejected before anything is installed")
	}
	if err := run(options{kindle: root, noSpotify: true, noCalendar: true, timezone: "Mars/Olympus", yes: true}, u); err == nil {
		t.Error("an unknown time zone should be rejected")
	}
	if _, err := os.Stat(filepath.Join(root, installDir)); err == nil {
		t.Error("nothing should have been written")
	}
}
