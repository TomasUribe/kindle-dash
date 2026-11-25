# Security and privacy

## What is stored, and where

Everything stays on **your computer and your Kindle**. There is no server and no telemetry.

| Secret | Where it lives | Notes |
|---|---|---|
| Spotify refresh token | `kindle-dash/spotify_token.json` on the Kindle | Lets the Kindle control your Spotify. No client secret is used (PKCE login). |
| Secret calendar addresses | `kindle-dash/config.json` on the Kindle | Anyone who has an address can read that calendar. |
| Spotify Client ID | `kindle-dash/config.json` | Not a secret. |

The Kindle's drive is plain storage: **anyone who can plug your Kindle into a computer can read these files.**
Do not lend the Kindle to someone you would not trust with your calendar, and remember the Kindle's own screen
passcode does not protect the USB drive.

## Taking access back

* **Spotify**: open your Spotify account page, *Apps*, and remove your app. Or delete the app in the developer dashboard.
* **Google Calendar**: Settings, the calendar, *Integrate calendar*, **Reset** next to the secret address.
* Or run `kdash-setup -remove` with the Kindle plugged in.

## What the setup program does and does not do

* It never prints your calendar addresses back, and its error messages leave them out.
* Your Spotify password is only ever typed on Spotify's own page. The program only receives a login code on
  `http://127.0.0.1:8888/callback`, a port that only your own computer can reach.
* It writes only inside the `kindle-dash` folder and the two `Kindle Dash*.sh` files in `documents` on the Kindle.
* It asks Spotify for the smallest set of permissions the app uses (read and control playback, read your playlists,
  saved music and recent plays). It cannot change your library or playlists.

## Reporting a problem

If you find a security problem, please open a GitHub issue **without** including any tokens or calendar addresses,
or use GitHub's private vulnerability reporting for this repository.
