# Song Sleeve

A local-first music player with an opt-in cloud library. The product and technical
design live in [docs/implementation-plan.md](docs/implementation-plan.md); the screen
designs are in [designs/](designs/).

This repository currently holds the **server** (milestones M0–M3) and the **web app**
(M3, owner-only). The desktop and Android apps (M4, M5) and the public landing page
(M6) are not started yet; see [Status](#status).

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
  doesn't interfere. Audio fixtures are generated with ffmpeg.
- **Browser (`make e2e-web`):** sign in as `owner@example.com` on the dev database and
  add songs to it; they reset the local rate-limit counters first. Set
  `E2E_DELETE_EMAIL` to an allowlisted throwaway account to also run the account
  deletion test.

## Layout

```text
apps/web            React + Vite web app (cloud-only, R-WEB)
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

**Not started:** the desktop app (Wails) and installed-app core with local songs,
the `Songs` folder and folder check (M4); the Android app (M5); the landing page,
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
