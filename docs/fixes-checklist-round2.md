# Fixes checklist (desktop test round 2)

Source: the user's test of the round 1 fixes on 2026-10-20. Screenshots are in `fixes-evidences/`,
named after the problem. Items 12–14 came from the user's message, without a screenshot.
Status: **all 14 implemented on 2026-10-20, not committed.** Design approved from `designs/proposals/Round2.html`; see [Implementation notes](#implementation-notes). Round 1 is in `fixes-checklist.md`.

Most items are in the shared web UI (`apps/web/src`), so each fix also applies to the web app.
Size: **S** = CSS or a few lines, **M** = one component, **L** = new feature or several files.

---

## Player bar

### [x] 1. The play icon is off-centre (S)
- **Evidence:** `play-icon-uncentered.png`
- **Cause:** the `play` path (`components/Icon.tsx:269`) already sits right of centre to balance the
  triangle (x 7–21 in a 24 box). On top of that every caller adds `marginLeft` 2–4 px:
  `PlayerBar.tsx:95` (3), `Playing.tsx:114` (4), `SongRow.tsx:155,159` (2). Together that's about 4 px
  too far right.
- **Fix:** remove the extra margins. Then check the bar, the full player and the row thumbnails at
  2× zoom. If the triangle still looks off, nudge the path itself once, so no caller needs a margin.

### [x] 2. Clicking the artist name in the bar opens the full player (S–M)
- **Evidence:** `clicking on artist name on now playing open full screen and dont navigate to artist page.png`
- **Cause:** the sleeve, title and artist are all inside one `<Link to="/playing">` (`PlayerBar.tsx:72`).
  The player's current track carries only the artist's name (`controller.ts`, `artist: string | null`),
  with no id to link to.
- **Fix:**
  - Add `artistId` to the player's current track, filled from `track.artist_id`.
  - Split the block: the sleeve and the title stay a link to `/playing`.
  - The artist becomes its own `<Link to="/artists/{id}">`, underlined on hover like the artist links
    in song rows. With no artist, it stays plain text.
  - Do the same for the artist line in the full player (`Playing.tsx`).

### [x] 3. Move the origin pill next to the song name (S)
- **Evidence:** `origin of song played can move to the right of the now playing bar at the side of the name and artist.png`
- **Cause:** layout. The pill is in `.pbar-side`, at the far right (`PlayerBar.tsx:121`).
- **Fix:**
  - Render the pill inside the left block, after the title and artist.
  - It keeps `min-width: 96px`. The title and artist ellipsize first.
  - The pill is hidden on narrow screens, as `.pbar-side` is today.

### [x] 4. A ⋯ menu for the current song in the bar (M)
- **Evidence:** `three dots menu should exits on now playing bar betwehn origin location and fullscreen btn.png`
- **Cause:** missing feature. The song menu exists only inside `SongRow` (`components/SongRow.tsx`).
- **Fix:**
  - Pull the song's menu items out of `SongRow` into a shared `SongMenu({track, ctx, anchor, onClose})`.
    The rows, the bar and right-click (item 5) all use it.
  - Add a `round-btn` ⋯ to `.pbar-side`, before the expand button. Once item 3 moves the pill, the
    order is name · origin pill … ⋯ · expand.
  - It's disabled when nothing is playing. It has the tooltip "More actions".

---

## Right-click

### [x] 5. Right-click shows the browser's menu instead of the item's menu (M–L)
- **Evidence:**
  - `right clicking on a song should open the menu (…).png`
  - `right clicking on a playlist and or groups is showing the native menu (…).png`
- **Cause:** there's no `contextmenu` handler anywhere, so WebKitGTK shows its own menu (Back,
  Reload, Inspect…). Also:
  - `Menu` only opens from an anchor element, not at a point.
  - The playlist and group menus are written inline in `Playlist.tsx` and `Group.tsx`, and library
    cards have no menu at all.
- **Fix:**
  - `Menu` takes `anchor: RefObject | {x, y}` and keeps its existing flip and clamp logic.
  - Pull out shared `PlaylistMenu` (rename, add to groups, delete) and `GroupMenu` (rename, delete),
    used by the page's ⋯ button and by right-click.
  - `onContextMenu` opens the right menu at the pointer on:
    - song rows (all lists) and song cards (Home, Library);
    - playlist and group cards (Home, Library);
    - the sidebar's playlist and group links;
    - the playlist rows on a group page (they keep their in-group menu: rename, remove from group,
      delete).
  - Desktop only: one `contextmenu` listener on `document` blocks the native menu everywhere else.
    It allows it in text inputs, so copy and paste still work. The web leaves the browser menu alone
    outside our items.

---

## Navigation

### [x] 6. No way back from a playlist to the group it was opened from (S–M)
- **Evidence:** `now back button to go back from playlist to group (…).png`
- **Cause:** the breadcrumb is fixed text, "Library / Playlists" (`Playlist.tsx:43`). "Playlists" isn't
  a link, and nothing records where the user came from.
- **Fix:**
  - Group rows link with `state: {fromGroup: group.id}`.
  - When that state is set, the playlist breadcrumb reads "Library / {Group name} / {Playlist}",
    with the group linked.
  - Add a round back button (←) before the breadcrumb on playlist and group pages. It runs
    `navigate(-1)` when there's history, otherwise it goes to the group or the library.
  - "Playlists" and "Groups" link to the library filtered to that kind.

---

## Add songs dialog

### [x] 7. Clicking the artist field should list every artist (S)
- **Evidence:** `clicking on artist input should show all existing artist until typing something filters it.png`
- **Cause:** `searchArtists` returns `[]` for an empty query (`packages/core/src/artistMatch.ts:98`), so
  the list opens empty until you type.
- **Fix:**
  - In `ArtistField`, when the field is open and empty, show every artist by name, scrolling inside
    the 280 px list.
  - Typing filters as today.
  - The "Use 'X'" suggestion stays the first option.

### [x] 8. The artist list is clipped inside the dialog and adds height (M)
- **Evidence:** `artist menu on add song dialog is staying below the rest of the page, (…).png`
- **Cause:** in compact mode the list is `position: absolute` inside the dialog's scrolling song list
  (`ArtistField.tsx:163`). That list has `overflow-y: auto`, so it clips the dropdown and grows its
  scroll height. The dialog's own sections then cover it.
- **Fix:**
  - Render the list in a portal with `position: fixed`, anchored to the input with the same
    measure, flip and clamp code as `Menu`. If there's no room below, it opens above.
  - Use z-index 55, above the dialog.
  - Close it when the dialog's list scrolls or the window resizes.
  - Keep the input's `aria-controls`/`aria-activedescendant`; the keyboard works as it does now.

### [x] 9. Add the songs to several playlists at once (M–L)
- **Evidence:** `playlist picker on add songs dialog should be a checkbox list (…).png`
- **Cause:**
  - `PlaylistPicker` is a single-choice `Select`.
  - The web dialog already sends `playlistIds: string[]` (`dialogs/AddSongsDialog.tsx:101`).
  - The desktop one sends a single `playlist_id`, and the Go side takes one id
    (`library.Import(ctx, items, playlistID string)`, `internal/library/library.go:240`;
    `shell/local.go:460`).
- **Fix:**
  - **Frontend:** `PlaylistPicker` becomes multi-select. The trigger reads "None", "Tavern Tracks" or
    "2 playlists". Its menu lists the playlists as `menuitemcheckbox` rows that stay open when
    toggled, plus "New playlist…", which creates the playlist and checks it. The label becomes
    "Add to playlists (optional)".
  - **Desktop Go:** `Import(ctx, items, playlistIDs []string)` adds the songs to each playlist.
    `/local/import` accepts `playlist_ids`, and still reads `playlist_id` for older callers.
    Update `library_test`.

### [x] 10. Add songs from the Library and playlist pages, with the playlist preset (S–M)
- **Evidence:** `add songs dialog should have a link from library and playlist pages (…).png`
- **Cause:** only Home and Songs open "Add songs". The dialog already accepts `playlistId`
  (`app/dialogs.tsx:4`), but no page passes it.
- **Fix:**
  - Library gets the standard header's "Add songs" (item 12).
  - On a playlist page, the "Add songs" button becomes a small menu:
    - "Add files from this computer…" opens `{kind:'addSongs', playlistIds:[id]}`, with the playlist
      already checked.
    - "Choose from your songs…" opens today's picker (`addSongsToPlaylist`).
  - The playlist's empty state offers both.
  - On an artist page, "Add songs" presets that artist: a new `artistId` on the spec fills each new
    row's artist when the file has no tag.

### [x] 11. "Add to groups" can't create a new group (S–M)
- **Evidence:** `add playlist to group should allow creating new groups like creating playslist for songs.png`
- **Cause:** `PickerDialog` lists existing groups only (`dialogs/PickerDialog.tsx:56`).
- **Fix:**
  - For `addToGroups`, add a "New group…" row at the top. It turns into a name field with
    Create and Cancel, as in `PlaylistPicker`.
  - Create POSTs `/me/groups`. The new group appears in the list already checked, so "Add" saves it
    with the others.
  - The empty state (no groups yet) shows the same field.

---

## Page structure (no screenshot)

### [x] 12. One header layout on Songs, Artists and Library (M)
- **Request:** title, subtitle, then the actions at the end: the sync control (item 14) and
  "Add songs".
- **Today:**
  - **Songs:** title, count, a status card, "Retry failed" and "Add songs".
  - **Artists:** title and count, with no actions. Rename and delete sit on the artist card.
  - **Library:** the title has the "Filter saved…" note as its subtitle. Create playlist, Create group
    and the three filter chips share one row, and the chips are cut off when the window is narrow
    (visible in the right-click screenshot).
- **Fix:** a shared `PageHeader({title, sub, actions})` built on `.page-head`, which already wraps on
  narrow screens.
  - **Songs:**
    - Subtitle: "3 songs · 2 only on this computer" (the status card's text).
    - Actions: sync control (library) and Add songs.
    - "Retry failed" stays and appears only when something failed.
  - **Artists:**
    - Subtitle: "N artists".
    - Actions: sync control (library) and Add songs. Add songs presets the selected artist
      (item 10).
  - **Library:**
    - Subtitle: "N songs · N playlists · N groups".
    - Actions: sync control (library) and Add songs.
    - Below the header, one toolbar row: search, then the filter chips, then Create playlist and
      Create group.
    - "Filter saved to your account" becomes the chips' tooltip.

### [x] 13. The playlist page uses the group page's layout (S–M)
- **Request:** big art with the name beside it, the controls below, and sync and ⋯ at the end of the
  header.
- **Today:** `Group.tsx` already does this: sync and ⋯ sit at the right of the header, with Play and
  Shuffle below. `Playlist.tsx` puts every button in one row under the header.
- **Fix:**
  - Move the playlist's sync control and ⋯ into the header's right column, as on the group page.
  - The row below keeps Play, Shuffle, Add songs (item 10) and the drag hint.
  - Both pages use the same CSS class (`.coll-head`) instead of duplicated inline styles, so they
    can't drift apart again.

---

## Sync control (no screenshot)

### [x] 14. The sync button becomes a switch plus a details button (M)
- **Request:**
  - The switch turns automatic sync on or off for the current page. Anything added there later
    syncs too.
  - The details button opens today's dialog, with the same switch inside.
- **Today:** `CloudChip` (playlist and group) and the Songs status card are buttons that open
  `SyncScopeDialog`. Inside it, the user confirms with "Sync to cloud" or "Pause automatic sync".
- **Fix:** a `SyncControl({scope, id?})` in place of `CloudChip` and the status card.
  - **Switch:** `button.switch[role=switch][aria-checked]`. The `.switch` styles already exist in
    `app.css:505`. It's labelled "Auto sync" with a cloud icon.
    - On: `PUT /me/sync-scopes/{scope}[/{id}]`, with a toast "N songs queued to sync" (or "Sync on").
    - Off: `POST …/pause`, with the toast "Automatic sync paused".
    - It reflects `catalog.scopes` and shows busy while the request runs.
  - **Details:** an icon button (info) with a small badge while songs are uploading. It opens
    `SyncScopeDialog`.
  - **Dialog:** the same switch at the top, then the stats (already synced, to upload or in queue,
    need attention) and the notes, which update live as the switch flips. The two action buttons
    are replaced by Close.
  - Without `cloud_sync_enabled`, the switch is disabled, with the tooltip "Cloud sync isn't
    available on this account".
  - **Which scope each page uses:**
    - Songs, Artists and Library: `library`. The server has no artist scope, so on Artists the
      switch is the library's.
    - Playlist: `playlist/{id}`.
    - Group: `group/{id}`.
  - **Web:** shown too (decision A).
  - **Turning on:** applies at once, no confirmation (decision B).

---

## Decisions (2026-10-20)

- **A. The web shows the sync control too.** A scope set on the web applies to every installed
  computer. On the web, the dialog says "Your installed apps will upload songs they add here", and
  the stats count the songs that are on a computer but not in the cloud yet.
- **B. The switch turns sync on at once.** No confirmation step. The toast says how much will upload
  ("12 songs queued to sync · 340 MB"), with a "Details" link that opens the dialog.

Design preview to review before implementing: `designs/proposals/Round2.html`.

## Implementation notes

- **New shared pieces:** `components/SyncControl.tsx` (switch + details), `app/syncScope.ts`
  (scope tracks, stats, toggle), `components/PageHeader.tsx`, `components/CollectionHeader.tsx`,
  `components/CollectionMenus.tsx` (playlist/group menus, `WithContextMenu`), `groupQueue` in core.
  `CloudChip.tsx` is gone.
- **Menus:** `Menu` accepts a point anchor; `useContextMenu` opens it at the pointer. Right-click menus
  for songs, playlists and groups start with Play.
- **Web sync dialog:** the web has no local songs, so it shows synced / syncing or waiting / need
  attention, and the note "Your installed apps will upload the songs they add here" (no "to upload").
- **Player bar:** the artist link uses the catalog track's `artist_id`; no change to the player
  controller was needed. The full player already linked the artist.
- **Desktop import:** `/local/import` takes `playlist_ids` (still reads `playlist_id`).
  `Library.Import` takes a list and is all or nothing; new test `TestImportIntoSeveralPlaylists`.
- **Artist preset:** opening Add songs from an artist fills that artist in, except for files with
  their own artist tag, which show the tag as the hint.
- **Checked:** typecheck (3 packages), `make test`, Go vet including integration tags,
  web Playwright 5 passed / 1 skipped (one step updated for the new Add songs menu),
  `make desktop-build`, and screenshots of every changed web screen.
- **Not checked in the desktop window:** a second copy of the app can't start while a dev
  instance is running (single-instance lock), so the WebKit right-click block and the switch
  with local songs need a manual check.
