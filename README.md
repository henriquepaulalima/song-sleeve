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
keyring daemon).

Operator CLI (§10.5): `make admin ARGS="usage owner@example.com"`, also `set-quota`,
`cloud-sync on|off`, `suspend`, `unsuspend`, `delete-account`, `stalled`.

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
  server, offline start, purge) against the same real API.
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
from this device. Not yet: a tray icon, native MPRIS when WebKitGTK's Media Session
doesn't provide it, the GStreamer codec check, crash reports, cleanup credentials,
installers (AppImage, `.deb`, Windows) and auto-update. Windows isn't tested.

**Not started:** the Android app (M5); the landing page,
final Terms/Privacy pages (drafts are linked for now), Sentry, observability and
release work (M6). The cleanup-credential endpoints exist, but credentials are only
issued once the installed apps exist.

**Deviations from the plan's proposals (§4.1, §4.3):**

- Local object storage is **SeaweedFS** instead of MinIO: MinIO's images are no
  longer pullable from Docker Hub. The server talks plain S3 through the
  `ObjectStore` interface, so nothing else changes.
- SQL is hand-written with pgx instead of generated with sqlc, and there is no
  OpenAPI spec or generated client yet; the TypeScript API types in
  `packages/core/src/api/types.ts` mirror the Go handlers by hand.
- The Vercel config (`apps/web/vercel.json`) is a placeholder; nothing is deployed.
