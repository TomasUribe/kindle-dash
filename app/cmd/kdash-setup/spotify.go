package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Spotify login: Authorization Code with PKCE. There is no client secret anywhere: the person logs in on
// Spotify's own page, Spotify redirects to this computer, and only a refresh token is kept.

const (
	redirectHost = "127.0.0.1:8888"
	redirectURI  = "http://" + redirectHost + "/callback"
)

var (
	spotifyAuthURL  = "https://accounts.spotify.com/authorize"
	spotifyTokenURL = "https://accounts.spotify.com/api/token"
)

// spotifyScopes is everything the Kindle program uses and nothing else.
var spotifyScopes = []string{
	"user-read-playback-state", "user-modify-playback-state", "user-read-currently-playing", // now playing and controls
	"playlist-read-private", "playlist-read-collaborative", "user-library-read", // playlists, albums, liked songs
	"user-read-recently-played", // the Recent list
}

// tokenFile is what the Kindle program reads (spotify_token.json).
type tokenFile struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresAt    float64 `json:"expires_at"`
	Scope        string  `json:"scope"`
}

func randomURLSafe(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err) // no randomness means no safe login
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizeURL(clientID, challenge, state string) string {
	q := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"code_challenge_method": {"S256"},
		"code_challenge":        {challenge},
		"scope":                 {strings.Join(spotifyScopes, " ")},
		"state":                 {state},
	}
	return spotifyAuthURL + "?" + q.Encode()
}

// spotifyLogin runs the whole login. open is called with the page to show the person.
func spotifyLogin(ctx context.Context, clientID string, listenAddr string, open func(url string)) (*tokenFile, error) {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("could not listen on %s (is another program using that port?): %w", listenAddr, err)
	}
	defer ln.Close()

	verifier, state := randomURLSafe(64), randomURLSafe(16)
	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var res result
		switch {
		case q.Get("state") != state:
			res.err = errors.New("the answer did not match this login attempt (state mismatch)")
		case q.Get("error") != "":
			res.err = fmt.Errorf("Spotify said: %s", q.Get("error"))
		case q.Get("code") == "":
			res.err = errors.New("Spotify sent no login code")
		default:
			res.code = q.Get("code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if res.err != nil {
			fmt.Fprint(w, "<h2>Login was not completed.</h2><p>Go back to the terminal for details.</p>")
		} else {
			fmt.Fprint(w, "<h2>Spotify connected.</h2><p>You can close this tab and go back to the terminal.</p>")
		}
		select {
		case done <- res:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()

	open(authorizeURL(clientID, pkceChallenge(verifier), state))

	var res result
	select {
	case res = <-done:
	case <-ctx.Done():
		return nil, errors.New("timed out waiting for the Spotify login")
	}
	if res.err != nil {
		return nil, res.err
	}
	return exchangeCode(ctx, clientID, res.code, verifier)
}

func exchangeCode(ctx context.Context, clientID, code, verifier string) (*tokenFile, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", spotifyTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Spotify: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &tr)
	if resp.StatusCode != 200 || tr.AccessToken == "" || tr.RefreshToken == "" {
		msg := tr.Error
		if tr.Description != "" {
			msg += ": " + tr.Description
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("Spotify refused the login (%s)", msg)
	}
	return &tokenFile{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken,
		ExpiresAt: float64(time.Now().Unix() + int64(tr.ExpiresIn)), Scope: tr.Scope}, nil
}

// openBrowser tries to show a page in the person's browser; the address is printed as well, so failure is fine.
func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}
