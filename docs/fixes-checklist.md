# Fixes checklist (desktop test round 1)

Source: the user's test of the desktop app on 2026-10-15. Screenshots are in `fixes-evidences/`.
Status: **all 21 implemented on 2026-10-15, not committed.** See [Implementation notes](#implementation-notes). Each item lists the cause, the proposed fix and
the files involved. Most items are in the shared web UI (`apps/web/src`), so the fix also applies to
the web app.

Decisions were answered on 2026-10-15; see [Decisions](#decisions).

Legend for size: **S** = CSS or a few lines, **M** = one component, **L** = new feature or several files.

---

## Menus and dialogs

### [x] 1. Every menu item has the same icon (S–M)
- **Evidence:** `all-menu-options-with-same-icon.png`
- **Cause:** `MenuItem` has no icon prop. It always draws an 8 px dot (`components/Menu.tsx:123`,
  `.mi-dot` in `styles/app.css`). Delete gets an orange dot; every other item gets a sand one.
- **Fix:** add an `icon` prop to `MenuItem`. When it's set, draw the icon (18 px, sand; orange for
  `danger`) instead of the dot. Pass an icon at every call site:

  | Item | Icon |
  |---|---|
  | Edit, Rename (playlist, group) | `pencil` (exists) |
  | Add to playlist | `playlistAdd` (new: list with a plus) |
  | Replace audio file | `fileSwap` (new) or `file` (exists) |
  | Sync to cloud | `cloudUp` (exists) |
  | Retry sync | `retry` (exists) |
  | Cancel sync / cancel queued / cancel replacement | `close` (exists) |
  | Show in folder | `folderOpen` (new) |
  | Relink | `link` (new) |
  | Use changed file / Review suggestion | `check` / `info` (exist) |
  | Move to top / Move to bottom | `arrowTop` / `arrowBottom` (exist) |
  | Remove from playlist / Remove from group | `minus` (exists) |
  | Add to groups | `folderPlus` (new) |
  | Delete song / playlist / group | `trash` (exists) |
- **Files:** `components/Menu.tsx`, `components/Icon.tsx` (new icons), `components/SongRow.tsx`,
  `screens/Playlist.tsx`, `screens/Group.tsx`, `apps/desktop/frontend/src/platform.ts`, `styles/app.css`.

### [x] 4. The playlist picker in Add songs opens on top of other apps (M)
- **Evidence:** `menu-from-playlist-overlaping-apps-and-not-following-style.png`, `no-option-to-create-playlist.png`
- **Cause:** "Add to a playlist (optional)" is a native `<select>`
  (`apps/desktop/frontend/src/AddSongsDialog.tsx:185`, also in the web dialog at
  `dialogs/AddSongsDialog.tsx:236`). WebKitGTK draws a native `<select>` list as a separate GTK popup
  window placed in screen coordinates, outside the app window. That is why it can end up over the
  file manager.
- **Fix:** replace it with a `Select` component built on the existing `Menu` popover. The popover
  is rendered inside the page, so it stays in the window and matches the other menus (this also
  fixes item 7). Use the same component for the language `<select>` in `components/common.tsx:47`,
  which has the same problem on the desktop Start and Login screens.
- **Files:** new `components/Select.tsx`, both `AddSongsDialog.tsx` files, `components/common.tsx`.

### [x] 7. The playlist picker in Add songs uses the default system style (covered by 4)
- **Cause:** same native `<select>` as item 4. The open list is drawn by GTK, so app CSS can't style it.
- **Fix:** the new `Select` in item 4 uses `.menu`/`.mi` styles: well background, rounded corners,
  a check mark on the selected playlist.

### [x] 6. No way to create a playlist from "Add to playlist" (M)
- **Evidence:** `no-create-playlist-on-songs-list.png`, `no-option-to-create-playlist.png`
- **Cause:** the song menu's submenu only lists existing playlists, or shows "No playlists yet"
  (`components/SongRow.tsx:229-231`). The Add songs picker only lists existing playlists.
  Creating a playlist only exists on Library (`dialogs/NameDialog.tsx`).
- **Fix:**
  - **Song menu:** add a **New playlist…** item at the top of the submenu (with a plus icon, above
    a divider). It opens the create dialog with the song attached
    (`{ kind: 'createPlaylist', addTrackId }`). After the server creates the playlist, the dialog
    adds the song and shows a toast "Added to {name}" instead of navigating to the new playlist.
    For local songs, adding goes through the overlay route as it does today.
  - **Add songs dialog:** the app shows one dialog at a time, so opening the create dialog would
    close Add songs. Instead, make **New playlist…** the last option of the new `Select`. It turns
    the field into an inline name input with **Create**. The new playlist is then selected.
- **Files:** `components/SongRow.tsx`, `app/dialogs.tsx`, `dialogs/NameDialog.tsx`, `components/Select.tsx`,
  both `AddSongsDialog.tsx` files, i18n (`menu.newPlaylist`, `add.newPlaylist`, `toast.addedTo`).

### [x] 11. The artist suggestion overlaps the next row (S)
- **Evidence:** `overlaping-artist-menu.png` ("Use 'Nymeria'" drawn over the row below)
- **Cause:** in compact mode (the Add songs rows), the dropdown container is `position: absolute`
  (`components/ArtistField.tsx:171`). When the field is focused but empty and there is a file-tag
  hint, the container holds only a `.opt` button. That button has no background, so its text floats
  over the next row.
- **Fix:** show the hint as small underlined text below the input, like form hint text
  ("Use 'Nymeria'"). Render it in normal flow, not as an absolute element, both when the field is
  closed (it's a dashed pill today) and when it's open. Keep the absolute dropdown only for the real
  results list, which has a background and a shadow. The "Did you mean" banner also becomes
  in-flow hint text in compact mode.
- **Files:** `components/ArtistField.tsx`.

### [x] 12. The scrollbar covers the remove buttons in Add songs (S)
- **Evidence:** `overlaping-scroll.png`
- **Cause:** the rows list is a scroll container (`maxHeight: 42vh; overflowY: auto`,
  `apps/desktop/frontend/src/AddSongsDialog.tsx:131`). The remove buttons sit right at its edge with
  no right padding, so the scrollbar is drawn on top of them.
- **Fix:** add `scrollbar-gutter: stable` and about 10 px of right padding to that container (and
  to the web dialog's). Also add a thin app-wide scrollbar style (`::-webkit-scrollbar`, which works
  in WebKitGTK) using the well and muted colours, so scrollbars match the dark theme everywhere.
- **Files:** both `AddSongsDialog.tsx` files, `styles/app.css`.

---

## Sidebar

### [x] 3. The sidebar scrolls with the page and the account link gets hidden (M)
- **Evidence:** `left-menu-scrolling.png`
- **Cause:** the layout doesn't limit its height. `.shell` has `min-height: 100vh`
  (`styles/app.css:708`), so the whole document scrolls. The sidebar is part of that document, so
  it scrolls away with the page and stretches to the page's height. The playlists and groups list
  (`.side-lists`) has no CSS rule at all, so each new playlist or group makes the sidebar taller.
- **Fix:** give the app shell a fixed height and scroll only the inner areas:
  - `.shell { height: 100vh }`.
  - `.shell-body { min-height: 0; overflow: hidden }`.
  - `.shell-main { overflow-y: auto }`: the page scrolls here, not the window.
  - `.sidebar { height: 100%; min-height: 0 }`.
  - `.side-lists { flex: 1; min-height: 0; display: flex; flex-direction: column }`.
  - Each list body: `overflow-y: auto; min-height: 0`.

  The brand, main nav and account link stay fixed. Only the Playlists and Groups lists scroll, each
  inside its own section (see item 21). The player bar becomes a normal row, so it no longer needs
  `position: sticky`. The narrow layout (sidebar stacked on top) keeps today's behaviour behind the
  existing breakpoint.

  **Check after the change:** anything that scrolls the window (scroll-to-top on route change,
  `scrollIntoView` in the reorderable list) must still work with `.shell-main` as the scroll
  container.
- **Files:** `styles/app.css`, `components/Sidebar.tsx`, `screens/AppShell.tsx`.

### [x] 17. Remove Profile from the sidebar nav (S)
- **Evidence:** `two-profile-links-in-the-same-menu.png`
- **Cause:** `components/Sidebar.tsx:23` lists `/profile` in the main nav, and the account link at the
  bottom also goes to `/profile`.
- **Fix:** remove the nav entry. Keep `aria-current` on the account link when on `/profile` and give it
  the active style there.
- **Files:** `components/Sidebar.tsx`, `styles/app.css` (`.side-acct[aria-current]`).

### [x] 10. The Settings icon should be a gear (S)
- **Cause:** the desktop nav entry uses the `laptop` icon (`apps/desktop/frontend/src/platform.ts:83`).
  There is no gear icon.
- **Fix:** add a `settings` (gear) icon to `components/Icon.tsx` and use it there.

### [x] 21. Make the Playlists and Groups lists collapsible and remember their state (M)
- **Cause:** the sidebar renders both lists with plain headings (`components/Sidebar.tsx:36-62`).
- **Fix:** make each heading a button with a chevron (`aria-expanded`, `aria-controls`) that
  collapses its list. Combined with item 3: an open list takes the space it needs and scrolls inside
  its own section, and a collapsed list shows only its heading. Remembering the state needs a
  decision (see [Decision A](#decisions)).
- **Files:** `components/Sidebar.tsx`, `styles/app.css`; for decision A option 2, also the server
  preferences and core.

---

## Song lists and Library

### [x] 5. Song rows don't look clickable (S–M)
- **Evidence:** `no-click-indicator-on-songs-list-item.png`
- **Cause:** only the thumbnail and title (`.song-main` button, `components/SongRow.tsx:95`) are
  clickable and have a pointer cursor. The rest of the row (artist column, status, time) has no
  pointer and doesn't play. `.song-row` has no hover style, and nothing shows that clicking plays
  the song.
- **Fix:**
  - **Hover style:** `.song-row:hover` uses the `--hover` background (the same token the Library
    cards use).
  - **Play icon:** on hover or keyboard focus, the thumbnail's note icon changes to a play icon. The
    row that's playing shows pause.
  - **Whole row clickable:** clicking anywhere on the row plays the song, except on the artist link,
    status badge actions, the ⋯ button and the drag grip. Use a click handler on the row that ignores
    clicks coming from interactive children. Keep the existing `<button>` as the keyboard and
    screen-reader target.
- **Files:** `components/SongRow.tsx`, `styles/app.css`.

### [x] 8. No "now playing" indicator on Home and Library cards (S–M)
- **Evidence:** `no-playing-indicator-on-library.png`. The screenshot is the Home page; the Library
  grid has the same gap.
- **Cause:** only `SongRow` shows the equalizer icon for the playing song. The cards (`SongSleeve`
  in `screens/Home.tsx:63`, `CardArt` in `screens/Library.tsx:174`) don't read the player state. The
  equalizer icon is also static in the rows.
- **Fix:**
  - Pass `playing` to the song cards. When a card is playing, it shows the equalizer icon on a sand
    background with a sand outline, the same as the playing row.
  - Animate the equalizer: three bars scaling with CSS keyframes, running only while the status is
    `playing`. Paused shows still bars, and `prefers-reduced-motion` turns the animation off.
  - Use the same animated icon in `SongRow`.
- **Files:** `components/Icon.tsx` (or a small `Equalizer` component), `components/Sleeves.tsx`,
  `screens/Home.tsx`, `screens/Library.tsx`, `components/SongRow.tsx`, `styles/app.css`.

### [x] 16. Song, playlist and group cards look too similar (M)
- **Evidence:** `songs-playlists-and-groups-to-difficult-to-differ-on-library.png`
- **Cause:** all three use the same square card shape with a thin line icon (note, list, layers),
  so they read as the same kind of thing at a glance.
- **Fix (your suggestion):**
  - **Songs** stay as they are.
  - **Playlists** use a 2×2 grid of song tiles, all four the same colour (`--well`). Tiles past the
    song count show no note icon.
  - **Groups** use a plain folder (no playlists inside) with the playlist count on its front.
  - Approved design: `designs/proposals/LibraryCards.html` (Library icon becomes books on a shelf).

  Use the same icons in the sidebar lists, Home sections, the Playlist and Group page heroes, and
  the menu icons from item 1. **Conflict:** the 2×2 grid is the sidebar's current **Library** icon,
  so Library needs a new icon (see [Decision B](#decisions)).
- **Files:** `components/Icon.tsx`, `components/Sleeves.tsx`, `screens/Library.tsx`, `screens/Home.tsx`,
  `screens/Group.tsx`, `screens/Playlist.tsx`, `components/Sidebar.tsx`.

### [x] 2. The sync card on Songs does nothing when clicked (L)
- **Evidence:** `clicking-on-sync-option-dont-do-nothing.png`
- **Cause:** "10 songs only on this computer" is a status card (a plain `div`,
  `screens/Songs.tsx:106`), but it looks like a button next to Add songs. The real gap is bigger: the
  design's **Sync to cloud** scope dialog (`designs/WebSyncCloud.dc.html`: "Sync library / playlist /
  group", already-synced / to-upload / need-attention counts, **Pause automatic sync**) isn't built
  on the desktop.
  - The shell already auto-syncs local songs in enabled scopes (`device.autoSync`, covered by
    `TestAutoSyncScope`), but no screen can enable a scope.
  - On desktop, the playlist page shows "In your cloud library" as a static chip
    (`screens/Playlist.tsx:77`). That's correct on the web, where everything is synced, but wrong for
    a playlist that holds local songs.
- **Fix:** build the scope dialog from the design and open it from:
  - the Songs card, which becomes a button for the library scope;
  - the playlist page's cloud chip, which becomes a button on the desktop;
  - the group page.

  Confirming calls `PUT /me/sync-scopes/{library|playlist|group}/{id}` through the proxy. The
  proxy's reconcile after a write makes the shell queue the eligible songs. Failed songs are left
  out (D-25). When the scope is already enabled, the dialog shows progress and **Pause automatic
  sync**. On the web, the chip stays static as today.
- **Files:** new `dialogs/SyncScopeDialog.tsx` (shared, with a desktop-only confirm path via
  `platform`), `screens/Songs.tsx`, `screens/Playlist.tsx`, `screens/Group.tsx`, `app/dialogs.tsx`,
  `app/platform.ts`, `apps/desktop/frontend/src/platform.ts`, i18n.

---

## Player

### [x] 13. Shuffle and repeat are hard to see when on (S)
- **Evidence:** `repear-and-shuffle-options-barlly-visible.png`
- **Cause:** `.toggle-on` only changes the icon colour to `--sand` (`styles/app.css:1463`), and the
  icon is normally `--white`. The two colours are almost the same.
- **Fix:** `.toggle-on { color: var(--orange) }` plus a 4 px orange dot under the icon. The dot
  means the state isn't shown by colour alone. The same applies to the Playing dial (`.dial .db.toggle-on`).
- **Files:** `styles/app.css`.

### [x] 18. The play button has a broken shadow when nothing is playing (S)
- **Evidence:** `play-indicator-broken-when-no-song-playing.png`
- **Cause:** `.pp` has an inner highlight (`box-shadow: inset 0 2px 0 rgba(255,255,255,.22)`).
  `.pp:disabled` (`styles/app.css:1458`) changes the background to grey but keeps that highlight,
  which shows as a light curve at the top of the grey circle.
- **Fix:** add `box-shadow: none` to `.pp:disabled`. Check `.dial .pp` on the Playing screen too.
- **Files:** `styles/app.css`.

### [x] 9. Player controls have no tooltips or keyboard shortcuts (M–L)
- **Evidence:** `no-tooltip-on-controls.png`
- **Cause:** the controls only have `aria-label`, which screen readers use but which never shows on
  screen. The app has **no keyboard shortcuts at all**. The only key handlers are in menus,
  dialogs, the artist field and drag-reorder. Media keys work through `navigator.mediaSession`
  (if WebKitGTK forwards them).
- **Fix:**
  - **Tooltip:** a small styled `Tooltip` (or a `data-tip` CSS pattern) that appears after about
    400 ms on hover and on keyboard focus, for example "Play (Space)", "Next (Ctrl+→)". Use it on
    every icon-only button: player bar, Playing dial, expand/minimize, ⋯ buttons, the playlist
    shuffle button.
  - **Shortcuts:** one global `keydown` handler in the app shell. It ignores keys while typing in an
    input, textarea, select or editable field, and while a dialog or menu is open. Proposed set
    ([Decision C](#decisions)):

    | Key | Action |
    |---|---|
    | Space | Play / pause |
    | Ctrl+→ / Ctrl+← | Next / previous song |
    | → / ← | Seek forward / back 5 s |
    | S | Shuffle on/off |
    | R | Repeat song on/off |
    | F | Open / close the full-screen player |
    | Esc | Close the full-screen player |
    | Ctrl+F or / | Focus the page's search field |
  - **Settings:** add a "Keyboard shortcuts" section on the desktop page that lists the same table
    from one shared definition, so the list and the handler can't drift. On the web there is no
    Settings page, so the list goes in Profile, or `?` opens a small help dialog.
- **Files:** new `app/shortcuts.ts` (definitions and handler), new `components/Tooltip.tsx`,
  `components/PlayerBar.tsx`, `screens/Playing.tsx`, `screens/AppShell.tsx`,
  `apps/desktop/frontend/src/Settings.tsx`, i18n.

### [x] 15. The playlist's shuffle button starts a new song (S)
- **Evidence:** `shuffle-btn-on-playlist-starts-other-song.png`
- **Cause:** it's built as **Shuffle play**: it calls `play(null, true)`, which restarts the queue
  shuffled (`screens/Playlist.tsx:74`).
- **Fix (as requested):**
  - The button only toggles the player's shuffle mode (`player.setShuffle(!shuffle)`), with
    `aria-pressed`, orange when on (item 13), and the tooltip "Shuffle".
  - **Play** then starts the playlist with the current shuffle mode.
  - If this playlist is already playing, toggling shuffles the rest of the queue and keeps the
    current song (the controller already does this).
  - Rename the label from `common.shufflePlay` to `player.shuffle`.
- **Files:** `screens/Playlist.tsx`; also the Artist page if it has the same button.

### [x] 14. Restarting the app forgets the song and position (M)
- **Evidence:** `restarting-forgout-song-progress.png`
- **This is a real gap, not just the Ctrl+C case.** The plan requires it: R-PLAY-5…8 / D-11 says
  "After a restart, installed apps restore the last player state, paused." `PlayerController` keeps
  everything in memory only, and no code saves or restores it.
- **Fix:**
  1. **Core:** add `PlayerController.snapshot()` and `restore()`. The snapshot holds the queue
     (original order and shuffled order), the current track, position, source, shuffle and repeat.
     `restore` drops tracks that no longer exist and loads the current one **paused** at the saved
     position. The engine needs a `load` that doesn't auto-play, plus `seek` before play.
  2. **Desktop:** save the snapshot to `accounts/<id>/player.json` through a new `PUT /local/player`
     shell endpoint, and read it with `GET /local/player`. Save on track change, play/pause, seek,
     shuffle or repeat changes, every 5 s while playing, and on window close. The periodic save is
     what keeps the position close when the process is killed with Ctrl+C, because the page gets
     no close event then. Restore it after the first catalog load in `startSync`. Clear it on
     logout and remove.
  3. **Web:** stays empty after reload, as the plan says (R-WEB).
- **Files:** `packages/core/src/player/controller.ts` (and tests), `apps/desktop/frontend/src/engine.ts`,
  `apps/desktop/frontend/src/services.ts`, `apps/desktop/internal/shell/local.go`, a shell test.

### [x] 19. Full-screen player adjustments (S–M)
- **Evidence:** `now-playing-fullscrenn-adjustments.png`
- **Cause and fix:**
  - **Duplicate status pills:** remove the "Shuffle off / Repeat song off" pills
    (`screens/Playing.tsx:119-122`). The dial already shows both states, and with item 13 they're
    clearly visible.
  - **Minimize button placement:** the minimize button is a chevron-down at the top left
    (`screens/Playing.tsx:28`). Move it to the bottom right, at the same spot as the expand button
    in the player bar (same size, `--well` background, same right/bottom offsets). Opening and
    closing then happen in the same place.
  - **Minimize icon:** add a `collapse` icon that mirrors `expand` (the two diagonal arrows pointing
    inward) and use it instead of the chevron. Tooltip: "Minimize (Esc)". The top-left corner is
    then empty, and the header keeps "Playing from" centred and the ⋯ menu on the right.
- **Files:** `screens/Playing.tsx`, `components/Icon.tsx`, `styles/app.css`.

### [x] 20. Show where the song was started from in the player bar, as a link (M)
- **Cause:** the bar only shows the source as text inside the subtitle ("Raney Shockne · from Songs",
  `components/PlayerBar.tsx`). The source also loses information:
  - Home, Library and Songs all start playback as `{ kind: 'library', label: 'Songs' }`, so it
    always says "Songs" and links to `/songs`.
  - A group has no source, because a group page doesn't start playback itself.
- **Fix:**
  - **Core:** extend `QueueSource` with the real origin: `kind: 'songs' | 'library' | 'home' |
    'playlist' | 'artist' | 'group'`, plus `id`, and a helper `sourceHref(source)`. Each screen passes
    its own kind.
  - **Player bar:** to the left of the expand button, add a small pill "From: Tavern Songs" (icon
    and label, with an ellipsis for long names). It links to the origin page.
  - **Playing screen:** the "Playing from" link uses the same helper.
  - **Subtitle:** keep just the artist.
  - **Group:** if you want "from group X", the group page needs a **Play** button that queues all
    of its playlists' songs ([Decision D](#decisions)). Otherwise a song started from a
    playlist inside a group shows the playlist.
- **Files:** `packages/core/src/player/controller.ts`, `components/PlayerBar.tsx`, `screens/Playing.tsx`,
  `screens/Home.tsx`, `screens/Library.tsx`, `screens/Songs.tsx`, `screens/Playlist.tsx`,
  `screens/Artists.tsx`, i18n.

---

## Decisions

Answered by the user on 2026-10-15:

- **A → per device, offline (local storage).** No server change.
- **B → proposal shown in `designs/proposals/LibraryCards.html`.** The main problem is telling the *Library grid cards* apart; the sidebar icons follow. Approved with adjustments: plain folder, playlist tiles all the same colour.
- **C → approved** as proposed.
- **D → yes.** The group page gets **Play** (and shuffle toggle); it queues every song of every playlist in the group, in group order then playlist order, de-duplicated (first occurrence wins), with source `{ kind: 'group', id }`.

Original questions:


- **A. Where to save the sidebar collapse state (item 21).**
  1. **Per device, in local storage** (recommended). This is a layout preference, like window size.
     It needs no server change and works offline.
  2. **Per account, like the Library filters.** This adds two fields to the server `preferences`
     (migration, API, core types, catalog sync), so the state follows you to every device.
- **B. A new Library nav icon (item 16)**, because the 2×2 grid becomes the playlist icon. Options:
  books on a shelf (recommended), a stack of records, or keep the grid for Library and use a
  "list with a cover" icon for playlists instead.
- **C. The keyboard shortcuts (item 9).** Is the proposed table OK, or do you want different keys
  (for example N/P for next/previous)?
- **D. Groups (item 20).** Should a group page get a **Play** button so "from group X" is possible,
  or is showing the playlist enough for now?

## Suggested order

1. Quick CSS fixes: 18, 13, 12, 17, 10.
2. Layout: 3, then 21.
3. Menus: 1, 4/7, 6, 11.
4. Lists and cards: 5, 8, 16.
5. Player: 15, 19, 20, 9, 14.
6. Scope sync: 2 (largest).

After each group: `pnpm -r typecheck`, `make test`, the web Playwright tests, and a pass in the
desktop window.

## Implementation notes

What differs from the proposals above, or was found while implementing:

- **21 (collapse state):** saved per device in local storage on the desktop. The web keeps it
  only until reload, because R-WEB says the web stores nothing on the device (the browser test
  for that rule still passes).
- **17 (Profile):** on narrow windows the account link shrinks to its avatar, so Profile stays
  reachable without the nav entry.
- **20 (origin):** the pill shows the origin's icon and name; the word "From" was dropped so it
  fits next to error notices. Home, Library, Songs, playlists, artists and groups each record their
  own origin. The group page got **Play** (all songs of its playlists, in order, each once) and a
  shuffle toggle.
- **14 (restore):** saved to `accounts/<id>/player.json` via `GET/PUT /local/player`; checked in the
  real window by killing the app with `kill -9` mid-song: it reopened paused at the last save.
- **2 (sync):** the scope dialog works for the library, playlists and groups on the desktop; checked
  end to end in the real window (3 local songs uploaded after confirming).
- **Found while testing:** menus opened inside dialogs were drawn under the dialog backdrop (menu
  z-index raised), and Escape in a menu inside a dialog closed the dialog too (fixed). Home's
  one-row previews let hidden cards' art bleed into the visible row (cards now clip). Song lists
  re-rendered every row on each player tick; they now re-render only when the current song changes.
