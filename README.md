# Song Sleeve

A local-first music player with an opt-in cloud library. The product and technical
design live in [docs/implementation-plan.md](docs/implementation-plan.md); the screen
designs are in [designs/](designs/).

This repository currently holds the **server** (milestones M0–M3), the **web app**
(M3, owner-only) and the **desktop app** (M4, in progress). The Android app (M5) and
the public landing page (M6) are not started yet; see [Status](#status).

## Run it locally

Prerequisites: Go 1.26+, Node 20.19+ with pnpm, Docker, and `ffmpeg`/`ffprobe` on the
PATH (the worker verifies uploads with ffprobe).

```sh
make setup        # docker compose up (PostgreSQL, S3, Mailpit) + pnpm install
make api          # API + job workers on :8088, applies migrations on start
make web          # web app on http://localhost:5173
```

Open http://localhost:5173 and **register with `owner@example.com`** (any password of
10+ characters). Only emails in `CLOUD_SYNC_ALLOWLIST` can use the web app
(R-WEB-ACCESS); copy `.env.example` to `.env` to change it.

| Service | URL |
| --- | --- |
| Web app | http://localhost:5173 |
| API | http://localhost:8088 (the web reaches it through the Vite proxy at `/api`) |
| Mailpit (catches every email, e.g. password reset) | http://localhost:58025 |
| S3-compatible storage (SeaweedFS) | http://localhost:58333 |
| PostgreSQL | `postgres://songsleeve:songsleeve@localhost:55432/songsleeve` |

Ports are offset from the defaults so they don't clash with other local services.

### Desktop app (Linux)

Needs the GTK/WebKit headers once: `sudo apt install pkg-config libgtk-3-dev
libwebkit2gtk-4.1-dev` (builds use the `webkit2_41` tag; `wails doctor` wrongly asks
for 4.0). With `make api` running:

```sh
make desktop        # builds the UI and opens the app against http://localhost:8088
make desktop-build  # production binary at dist/song-sleeve
```

Any email can register on the desktop; only allowlisted ones get **Sync to cloud**.
The app keeps its data in `~/.local/share/song-sleeve`, mirrors synced songs into
`~/Song Sleeve`, and stores the sign-in token in the system keyring. Override with
`SONG_SLEEVE_DATA_DIR`, `SONG_SLEEVE_FOLDER`, `SONG_SLEEVE_API`, and
`SONG_SLEEVE_FILE_CREDENTIALS=1` (token in a private file, for machines without a
keyring daemon). In development builds only, `SONG_SLEEVE_DEV_SYNC_DELAY=1m make
desktop` makes every sync wait that long before uploading, to try things mid-sync
(log out, cancel, quit) on a fast local network. With a Sentry DSN set, `SONG_SLEEVE_DEV_CRASH_TEST=1`
sends one test error from the Go side and one from the page at startup.

Operator CLI (§10.5): `make admin ARGS="usage owner@example.com"`, also `set-quota`,
`cloud-sync on|off`, `suspend`, `unsuspend`, `delete-account`, `stalled`.

### Desktop packages and updates (Linux)

```sh
make linux-packages DESKTOP_VERSION=0.2.0   # dist/release: Song-Sleeve-x86_64.AppImage, song-sleeve_amd64.deb
make try-desktop-update                     # a local update channel (below)
```

Both packages hold the same binary. They use the system's GTK, WebKitGTK 4.1 and
GStreamer (the `.deb` depends on them; the AppImage expects them, like any Wails
app), so the AppImage runs on distributions that ship WebKitGTK 4.1: Debian 12+,
Ubuntu 22.04+, Fedora 38+. The AppImage tools are pinned by SHA-256 in
`apps/desktop/packaging/linux/build.sh`.

Updates (§9.4, D-28): each GitHub Release carries `latest.json`, a manifest of the
build's files signed with the release key (ed25519). The app checks
`releases/latest/download/latest.json` 15 seconds after start, every 6 hours and from
**Settings → Check for updates**. The AppImage downloads the new file next to itself,
checks it against the signed manifest, swaps it in and offers **Restart to update**.
If its folder isn't writable it links to the release page instead. The `.deb` only
says a new version exists and links to the new package. Builds without an update URL
and key (`make desktop`, `make desktop-build`) never update.

**Try it locally:** `make try-desktop-update` builds version 0.1.1 into `dist/try`,
publishes 0.1.2 with a throwaway key on `http://127.0.0.1:8099`, and serves it until
Ctrl+C. With `make api` running, start `dist/try/Song-Sleeve-x86_64.AppImage`, open
Settings, press **Check for updates**, then **Restart to update**. Settings then
shows version 0.1.2. To try the update-required screen instead, set
`MIN_CLIENT_VERSION_DESKTOP=0.1.2` in `.env` and restart `make api`: 0.1.1 is refused,
the screen downloads 0.1.2, and after the restart the app syncs again. Versions
below the API's minimum (0.1.0 by default) are always refused.

**Releasing:** pushing a tag `vX.Y.Z` runs `.github/workflows/release-desktop.yml`,
which builds on Ubuntu 22.04 (oldest glibc), signs the manifest and publishes the
release. One-time setup:

1. `cd apps/desktop && go run ./cmd/release keygen -out ~/song-sleeve-keys`. Keep
   `update-key` offline as the backup (§10.2); losing it means the installed apps
   can't verify updates any more.
2. In the repository settings, add the secret `SONG_SLEEVE_UPDATE_KEY` (the
   contents of `update-key`) and the variables `SONG_SLEEVE_UPDATE_PUBKEY` (the
   contents of `update-key.pub`) and `DESKTOP_API` (the production API, e.g.
   `https://api.example.com/api`, once it's deployed).

## Tests

```sh
make test       # Go and TypeScript unit tests
make e2e        # Go integration tests: real API, workers, PostgreSQL and S3 in-process
make e2e-web    # Playwright browser tests (needs `make api` and `make web` running)
```

- **Unit:** order keys, password policy and hashing, field and email rules, media
  limits and codec mapping, version checks, crypto, email templates (Go); the artist
  matcher, order keys, player rules, catalog store and selectors, API client, sync
  service, message catalog parity, limits (TypeScript); the web upload queue.
- **Integration (`make e2e`):** accounts (gates, passwords, reset, change, sessions,
  terms, suspension, reauthentication, account deletion), the mutation contract,
  cross-account isolation, the upload pipeline in every format, rejections, stalls,
  replacements and their conflict suggestions, queued deletes, quota, the change
  feed and snapshot paging, ordering, artists, scopes, the event stream, and the admin
  CLI. They create and use their own `songsleeve_test` database, so a running dev API
  doesn't interfere. Audio fixtures are generated with ffmpeg. The desktop tests
  run the device sync service and the shell (proxy, local songs, overlay, media
  server, offline start, purge) against the same real API, and the tray against a
  stand-in tray host on a private D-Bus session (`dbus-run-session`).
- **Browser (`make e2e-web`):** sign in as `owner@example.com` on the dev database and
  add songs to it; they reset the local rate-limit counters first. Set
  `E2E_DELETE_EMAIL` to an allowlisted throwaway account to also run the account
  deletion test.

## Layout

```text
apps/web            React + Vite web app (cloud-only, R-WEB)
apps/desktop        Desktop app (M4, in progress): Wails shell (main.go) and Go core —
                    per-account SQLite, local songs and overlay, the Song Sleeve folder and
                    folder check, pure-Go audio prober, reconciliation, downloads, pins,
                    the sync queue, and internal/shell (API proxy, local data, media server)
apps/desktop/internal/tray, internal/update  Tray icon (Linux) and the self-updater
apps/desktop/cmd/release  Update key generation and the signed latest.json
apps/desktop/packaging/linux  AppImage and .deb packaging, local update channel
apps/desktop/frontend  The web screens with desktop modules swapped in at build time
                    (services, platform hooks, Add songs dialog) plus Settings
packages/core       Shared TS services: API client, catalog store, sync service,
                    player controller, smart artist matcher, order keys, i18n (en, pt-BR)
server/cmd/api      Gin API (-with-worker runs the workers in-process for development)
server/cmd/worker   River workers: verify/publish, cleanup, email, account deletion, sweeps
server/cmd/admin    Operator CLI
server/internal/    accounts, catalog, operations, events, deletion, media, storage, …
server/internal/db  Goose migrations (embedded)
```

## Status

**Built and tested (server, M0–M3):** registration without verification, login,
90-day sessions with cookie (web) and bearer (apps) transports, CSRF on cookie
writes, the web access gate and cloud-sync allowlist, the client version check
(426), argon2id with the 10,000-common-password blocklist, password reset (public
reset page, token in the URL fragment) and change, reauthentication and
irreversible account deletion with a River worker, rate limits in PostgreSQL, the
terms gate. The mutation contract (Idempotency-Key receipts, revisions, status
recovery), durable account events with ordered cursors, the change feed and
paged snapshot, SSE with LISTEN/NOTIFY fan-out. The upload pipeline: pending track
documents, quota reservations, multipart staging grants, heartbeats and coalesced
progress hints, ffprobe verification (MP3, AAC, FLAC, WAV by real codec), immutable
final keys, all-or-nothing failure, the 5-minute stall sweep, the per-track
operation slot with queued replacements and deletes, grant-expiry promotion,
replacement metadata with conflict suggestions, download grants, delayed cleanup of
retired content. Artists, playlists, groups, fractional ordering, the Library
preference, sync scopes, queue IDs. The admin CLI.

**Built and tested (web, M3):** every web screen from the designs (Start, Log in /
Register, Forgot / Reset password, Coming soon, Accept terms, Home, Library, Songs,
Artists, Playlist, Group, Playing, Profile) and their dialogs, in English and
Português (Brasil). Uploads run three at a time from the tab's queue; playback
fetches one whole song into a Blob (Media Session wired); pins keep the playing
audio across replacements and remote deletes; the catalog lives only in memory and
nothing is written to browser storage.

**In progress (M4):** the desktop app runs on Linux (Wails, WebKitGTK 4.1). The Go
core is built and tested: local import that keeps files where they are, offline
edits, relink and health, the playlist overlay and merged order, the Song Sleeve
folder with manifest and folder check, downloads, pins, and the restart-safe sync
queue with three workers. The shell reuses the web screens: the UI calls `/api` as
on the web, the shell forwards it with the token from the system keyring and answers
local-song calls itself; the catalog comes from the SQLite replica merged with local
songs, kept current by a Go listener that also runs while the window is minimized.
Local songs show **On this device**, with Sync to cloud, Try again, Relink, Use
changed file and Show in folder; Add songs picks files or folders natively; songs
play from their file (or the Songs folder, or a stream) through a loopback media
server with seeking; Settings has Check Songs folder; Profile has Log out and remove
from this device. A folder watcher runs the quick check a couple of seconds after
anything changes in the Song Sleeve folder, so songs deleted or edited by hand come
back and stray files go while the app runs. Logging out (or switching accounts) with
syncs running cancels them on the server first; plain Log out puts them back in the
queue for the next login. A tray icon (StatusNotifierItem) shows the window, plays, pauses,
skips and quits; while a tray host shows it, closing the window keeps the app running.
GNOME needs the AppIndicator extension for it; without a host, closing quits. System
media controls are WebKitGTK's own MPRIS (checked with 2.54), named "Song Sleeve". On
Linux, missing GStreamer decoders are found at startup and explained in a notice and
in Settings. Linux ships as an AppImage that updates itself and a `.deb` that
announces new versions, both from one binary and published by the release workflow
(see Desktop packages and updates). Crash reports (D-35) go to Sentry when a build
has a DSN (`DESKTOP_SENTRY_DSN`; development builds take `SONG_SLEEVE_SENTRY_DSN`):
panics in the Go side and errors in the page, the page's through the shell
(`/local/sentry`), all rebuilt from an allowlist and scrubbed of names, paths,
emails, URLs and tokens by `server/pkg/scrub` and its twin in `packages/core`
(shared cases). **Send crash reports** in Settings is on by default. Installed apps
get a cleanup credential at every login; it survives logout, so a deletion made
elsewhere purges this device's copy of the account (signed in or not) at the next
check, and the device acknowledges it. A plain 401 never deletes anything. Not yet:
everything Windows (tray, installer, updater; the hand-off is in the plan, §9.4.1).

**Not started:** the Android app (M5); the landing page,
final Terms/Privacy pages (drafts are linked for now), Sentry for the server and the
web app, observability and release work (M6).

**Deviations from the plan's proposals (§4.1, §4.3):**

- Local object storage is **SeaweedFS** instead of MinIO: MinIO's images are no
  longer pullable from Docker Hub. The server talks plain S3 through the
  `ObjectStore` interface, so nothing else changes.
- SQL is hand-written with pgx instead of generated with sqlc, and there is no
  OpenAPI spec or generated client yet; the TypeScript API types in
  `packages/core/src/api/types.ts` mirror the Go handlers by hand.
- The Vercel config (`apps/web/vercel.json`) is a placeholder; nothing is deployed.
