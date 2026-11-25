# Third-party material

Kindle Dash is MIT licensed (see `LICENSE`). It bundles or uses the following.

| What | Licence | Where |
|---|---|---|
| Ubuntu Font Family, © Canonical Ltd | [Ubuntu Font Licence 1.0](https://ubuntu.com/legal/font-licence) | `app/fonts/` (embedded unmodified in the Kindle program; notice in `UBUNTU-FONT-NOTICE.txt`) |
| `golang.org/x/image`, `x/sys`, `x/text` | BSD-3-Clause, © The Go Authors | Go dependencies (`app/go.mod`) |
| `github.com/arran4/golang-ical` | Apache-2.0 | Go dependency: reads iCal feeds |
| `github.com/teambition/rrule-go` | MIT | Go dependency: repeating events |
| Mozilla's list of trusted certificate authorities | MPL-2.0 | `app/cmd/kdash-setup/payload/cacert.pem`, copied to the Kindle so it can verify HTTPS sites (taken from Debian's `ca-certificates` package) |
| Go's `time/tzdata` | BSD-3-Clause | embedded time zone database |

The program draws to the screen with **FBInk** and the jailbreak's tools, which are already on a jailbroken Kindle;
nothing of theirs is included in this repository.

## Services it talks to

* **Spotify Web API**: now playing, controls, your library. You use your own Spotify app (Client ID).
* **Google Calendar**: only the secret iCal address you give it.
* **[LRCLIB](https://lrclib.net)**: free, community lyrics database. It is sent the track title, artist, album and
  length of the song being shown on the Lyrics screen. Switch it off with `"lyrics": false` in `config.json`.

The demo mode uses made-up calendar events and placeholder lyrics, and by default a prism picture drawn in code
(`app/art_prism.go`); the demo player lists a real album's track titles and lengths, which are plain facts. The
screenshots in `docs/screenshots` show one album's cover (the artwork belongs to its rights holders) purely to
illustrate how album art looks on the screen. It is not part of the program or its source, and it will be removed
from the repository if a rights holder asks. No real lyrics appear anywhere in this repository.

Kindle, Amazon, Spotify, Google and Pink Floyd are trademarks of their owners. This project is not affiliated with or
endorsed by any of them.
