# Fixes checklist (desktop test round 3)

Source: the user's test of the round 2 fixes on 2026-10-20. Screenshots are in `fixes-evidences/`,
named after the problem.
Status: **all 4 implemented on 2026-10-20, not committed.** Design approved from
`designs/proposals/Round3.html`; see [Implementation notes](#implementation-notes). Round 2 is in `fixes-checklist-round2.md`.

Size: **S** = CSS or a few lines, **M** = one component, **L** = new feature or several files.

---

### [x] 1. New sidebar layout (M)
- **Evidence:** `the left menu should how a new layout, setting go to the bottom above the profile, (…).png`
- **Request:**
  - Settings moves to the bottom, above the account.
  - Playlists and Groups become collapsible nav items styled like the other pages.
  - Artists becomes collapsible too.
- **Cause:** layout.
  - `Sidebar.tsx` adds Settings to the main nav (`platform.nav`).
  - Playlists and Groups are small-caps section headers (`SideSection`, `.side-h`), not nav items.
  - The artists aren't listed.
- **Fix:**
  - **Collapsible items.** `SideSection` becomes `SideGroup({to, icon, label, items})`, a normal
    `.nav` row (22 px icon and label) whose label is a link and whose chevron button toggles the list.
    The children are indented under a thin guide line.
    - Artists links to `/artists` and lists the artists A–Z, each with its initial.
    - Playlists links to `/library?show=playlists`.
    - Groups links to `/library?show=groups`.
    - Right-click on any child keeps its menu. Artists have no menu yet, so right-click does nothing
      there.
  - **Selection:** only one row looks selected. A child is highlighted when its page is open, and its
    parent row stays plain. An empty list shows its row without a chevron.
  - **Bottom:** `platform.nav` items (Settings on desktop) render in a footer above the account link.
  - **Search:** the trigger for item 2 goes at the top.
  - **Kept:** open and closed states stay per device on desktop under the same key, with `artists`
    added. The middle area scrolls.
  - **Narrow screens:** unchanged; the bottom bar keeps Home, Library, Songs, Artists and the account.

### [x] 2. Search anywhere (L)
- **Evidence:** `the search input should a floating dialog like the search from PrimeNg (…).png`
- **Request:** a floating search that opens anywhere in the app. Typing finds songs, artists,
  playlists and groups that match partly or completely; clicking a result opens its page.
- **Cause:** missing feature.
  - Search is a field inside Library and Songs only (`SearchField`). It filters that page with
    `matchesQuery`, which is a plain substring match.
  - The `search` shortcut (`app/shortcuts.ts`) only focuses that field.
  - Fuzzy matching exists only for artist names (`artistMatch.ts`: `words`, `editDistance`, `classify`).
- **Fix:**
  - **Core:** `searchCatalog(state, query, {perKind: 5})` in `packages/core`, with tests.
    - It folds case and accents.
    - Matches rank in this order: whole name, prefix, word prefix, anywhere, then close spelling
      (edit distance 1–2 by length, using the existing `editDistance`).
    - It returns ranked hits per kind with the matched ranges for highlighting.
    - Songs also match on their artist's name, ranked below a match on their title.
  - **Web:** `SearchPalette`, a dialog drawn by `Root`.
    - **Opening:** the sidebar trigger, `Ctrl K`, `Ctrl F` or `/`. On narrow screens, a search icon in
      the top bar.
    - **Layout:** an input, a row of tabs (All, Songs, Artists, Playlists, Groups, each with its
      count), then one list of matches, best first, with the matched letters in sand. In All, each row
      has a small kind label. The other tabs show every match of that kind. With nothing typed, only
      the field shows.
    - **Keys, like the Claude CLI:**
      - ↑↓ move through the list, which scrolls to keep the selection in view.
      - ←→ switch tabs and skip tabs with no matches. Clicking a tab works too. Switching resets the
        selection to the first row.
      - Enter opens and Esc closes.
      - Since ←→ belong to the tabs, the caret stays at the end. Typing, Backspace and Ctrl+A still
        work.
    - **A11y:** the input is a `combobox` controlling a `listbox`, with `aria-activedescendant`. The
      tabs are a `tablist`. The result count is announced politely.
    - **Narrow screens:** the tab row scrolls sideways.
    - **Opening a result:** artist, playlist and group results open their page. A song opens
      `/songs?focus=<id>`; Songs scrolls to that row (`IncrementalList` grows to include it) and flashes
      a highlight. Each song result has a ▶ that plays it at once from the library order (source
      Songs).
  - **Shortcuts:** the `search` shortcut opens the palette. `Ctrl K` is added to the list in
    Settings and Profile.
  - **Page search:** the search fields on Library and Songs are removed (decision C). The Library
    filter chips stay.
  - **i18n:** en and pt-BR.

### [x] 3. One Create button (S)
- **Evidence:** `create group and playlist should be menu options on a single create btn with a add icon only.png`
- **Cause:** `Library.tsx` draws two secondary buttons, Create playlist and Create group.
- **Fix:**
  - One round `+` button at the end of the Library toolbar, with the tooltip "Create" and a menu:
    New playlist (playlist icon) and New group (folder icon). Each opens today's name dialog.
  - With the search field gone (item 2), the toolbar is the filter chips plus this button.

### [x] 4. Opening the artist field changes the dialog's height (S)
- **Evidence:** `opening artist menu changes the height of the dialog-1.png`, `…-2.png`
- **Cause:** in the closed, compact state `ArtistField` shows a note under the chip ("New artist ·
  created when you save", `ArtistField.tsx`, `{compact && <span>{note}</span>}`). The open state has
  no note, so the row loses a line and the dialog shrinks and moves. The "Use 'X'" hint and
  "Did you mean…?" also come and go.
- **Fix:**
  - In compact mode, render one fixed-height slot (18 px, one line, ellipsis) under the field in both
    states.
  - The slot shows, in priority order: "Did you mean…?" with its link, the "Use 'X'" hint, the note,
    or nothing (the height stays either way).
  - The non-compact field (Edit song) keeps its current layout.

---

## Decisions (2026-10-20)

- **C.** The search fields on Library and Songs are removed; search anywhere replaces them.
- **D.** Enter or a click on a song **plays it at once** (library order, source Songs) **and** opens
  Songs scrolled to it with a brief orange highlight. The separate ▶ button was dropped, since Enter
  already plays.
- **Tabs.** All / Songs / Artists / Playlists / Groups; ←→ switch tabs, ↑↓ move, as in the Claude CLI.

## Implementation notes

- **Core:** `searchCatalog` in `packages/core/src/search.ts`, with 6 tests. It reuses `editDistance`
  and `typoBudget` from `artistMatch.ts`, which are now exported.
- **Web:**
  - `components/SearchPalette.tsx`, mounted as `SearchHost` in `Root`. Its open state is in
    `app/search.ts`.
  - `Ctrl K`, `Ctrl F` and `/` open it. The Ctrl shortcuts work even while typing in a field.
- **Songs:** `?focus=<id>` makes `ReorderableList` render up to that row (`revealId`), scrolls to it
  and flashes it once; the parameter is then cleared. Rows carry `data-track-id`.
- **Sidebar:**
  - It uses `Link` with its own `aria-current`, because `NavLink` would also select Library on
    `/library?show=playlists`.
  - Artists, Playlists and Groups fold, and the artists are listed.
  - Settings (from `platform.nav`) and the account are at the bottom.
  - Narrow bar: Home, Library, Songs and Artists, then a search icon, Settings (desktop) and the
    avatar.
- **Library:** one round `+` Create button with a menu.
- **Add songs:** the compact artist field has a fixed 18 px slot. The dialog measured 625.6 px
  closed, open and while typing.
- **Checked:**
  - typecheck (3 packages) and `make test` (core 69, web 10, Go)
  - Go vet including integration tags, and `make desktop-build`
  - web Playwright: 5 passed, 1 skipped (the create-playlist step now uses Create → New playlist)
  - screenshots of every changed screen at 1440 px and 390 px
- **Not checked in the desktop window:** the dev instance was running (single-instance lock).
