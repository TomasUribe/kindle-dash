# config.json

`kdash-setup` writes `kindle-dash/config.json` on the Kindle. You can edit it by hand (plug the Kindle in, edit the
file, eject). Running `kdash-setup` again changes only the things it asks about and keeps the rest. A made-up
example is in [`config.example.json`](config.example.json).

| Key | Default | Meaning |
|---|---|---|
| `calendars` | `[]` | List of `{ "name", "url" }`. `url` is a Google "secret address in iCal format". Any `.ics` feed works. |
| `spotify_client_id` | none | Client ID of **your** Spotify app. Empty hides the music features. |
| `timezone` | `UTC` | IANA name, e.g. `America/New_York`. |
| `clock24` | `true` | 24-hour clock. Also a switch in Settings on the Kindle. |
| `week_starts_monday` | `true` | First column of the month view. |
| `calendar_days` | `7` | Days always listed on the agenda. |
| `calendar_lookahead` | `60` | On a quiet week, how many days ahead to look for the next event. |
| `calendar_browse_back_months` | `12` | How many months back the Calendar screen can go. |
| `calendar_browse_ahead_months` | `24` | How many months forward it can go. |
| `calendar_ttl_seconds` | `300` | How long a downloaded calendar is reused. |
| `lyrics` | `true` | Fetch lyrics from LRCLIB. `false` sends nothing to LRCLIB. |
| `lyrics_lead_seconds` | `2.0` | How early the highlighted line moves, because the screen is slow. Page turns use at most 1 s of this, so a page never changes while its last line is still being sung. |
| `next_track_lead_seconds` | `4` | When Spotify says what plays next, its cover and title are put on screen this many seconds before the current song ends, so they are there when it starts (the screen is slow). Negative switches this off. Spotify does not say for playlists you do not own; then the app just checks every second from the moment the song should end. |
| `poll_seconds` | `10` | How often Spotify is asked what is playing while a music screen is showing (other screens ask much less often). |
| `progress_seconds` | `15` | The progress bar moves in steps this long (each step is a slow screen update). After you seek, the steps start from where you tapped. |
| `full_refresh_every` | `40` | Partial screen updates before a full black-white flash that clears ghosting. |
| `fbink_full_args` | `["-c","-f"]` | FBInk flags for a full refresh. |
| `fbink_partial_args` | `[]` | FBInk flags for partial updates (try `["-W","GL16"]` for a different waveform). |
| `spotify_token_file` | `spotify_token.json` | Where the Spotify login is kept (relative to the config). |
| `state_file` | `state.json` | Remembers the last screen, orientation, night mode, and so on. |

Lower numbers for `poll_seconds` and `progress_seconds` make the screen follow the music more closely, but cost
battery and make the e-ink flicker more.
