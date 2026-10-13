# Implementation plan review

**Reviewed:** `docs/implementation-plan.md` as of commit `81d7c05` (2026-10-07)
**Result:** the plan was restructured in place, then revised again after the owner's decisions on 2026-10-08 (see [Owner decisions, round 1](#owner-decisions-round-1-2026-10-08)). The original plan is preserved as [research/implementation-plan-v1.md](research/implementation-plan-v1.md). This file records what the review found, why the structure changed, and which findings still need a product decision.

## Summary

The plan's product decisions are mostly sound and consistent with each other. The main problems were:

1. **Structure.** Each rule was stated in several places: the "every source submission creates a fresh content ID" rule appears at least eight times and the "currently playing resource stays pinned" rule appears seven times. The wording differs slightly each time, which invites drift during implementation. Rules were also mixed into screen descriptions, API paragraphs, and test lists.
2. **Seven design gaps that would block or break implementation** (C-1 to C-7 below). The biggest one is that, under the current rules, an unsynced song can never be playable on two devices at once.
3. **The delivery plan's dependency order was inverted.** Phase 7 ("online-only mutations, revision checks, events") is the base that phases 1–3 depend on. Phase 4's exit criteria require native clients from phase 6. Phase 0 tests cloud playback before cloud sync exists.

The restructured plan states each rule once with a stable ID (`R-…`), adds one state model, groups the data model and API by module, collects every open decision in one table (`D-…`), and replaces the phase table with dependency-ordered milestones. Each milestone has its own exit tests.

## Critical findings (design flaws or contradictions)

| ID | Finding | Where | Resolution in restructured plan |
| --- | --- | --- | --- |
| C-1 | **Unsynced songs can't be playable on two devices.** Selecting a file on device B always creates new content (fresh ID). That makes device A's file a mismatch (`needs_relink`). When A relinks, it creates more new content and B mismatches again. The same loop happens during post-unsync recovery. | Track identity table, "Account-level updates" ¶4, unsync recovery | Added **R-SRC-6, "Locate file on this device"**: a device-local action that attaches a file only if its checksum and size match the *current* accepted content. It makes no server write and creates no content. **Replace audio file** keeps the fresh-content rule. Needs approval (**D-4**). |
| C-2 | **The lifecycle of candidate content is contradictory.** "Gin creates a staged content-version/object record" before upload, and the final key is named `songs/{contentVersionID}` before acceptance. Elsewhere, "delete old/create new content atomically at commit" and "clients never observe a dangling reference". | Storage §, bucket step 2, track identity ¶ | **R-ID-4**: the server allocates a *candidate* content ID inside the operation. Candidates are never referenced by `tracks` or exposed in the catalog. They become current only at the commit transaction. Abandoned candidates are garbage-collected. |
| C-3 | **Web copy mode and cloud sync conflict.** Web copy mode stores a managed OPFS copy, but "a synced track on web is always fetched from the bucket." The plan doesn't say whether the OPFS copy survives sync, is used for playback, or is deleted. | Product direction, storage § | **R-PLAY-3** (proposed): web plays a verified local OPFS copy of the *current* content when one exists. Otherwise it does the whole-song bucket fetch. **D-5**. |
| C-4 | **Desktop scope is undefined, but the rules require desktop behavior everywhere.** The stack says desktop packaging is "unselected," checklist item 7 says "if desktop installs are in scope," and the delivery plan has no desktop phase. | Stack review, checklist 7, delivery plan | **D-1**, a go/no-go decision. Desktop is a conditional milestone (M7). Rules say "installed client" (native mobile or desktop). |
| C-5 | **Deleting a track isn't covered by the per-track operation slot or by playback pins.** The slot covers upload, replace, unsync, and download, but not delete. If a track is deleted while another device is playing it, the plan doesn't say what that device does. | Serialized sync table, Playing screen | **R-SYNC-1** adds delete to the slot. **R-PIN-5**: a remote delete is deferred like unsync on other devices. On the deleting device, playback stops immediately. |
| C-6 | **Source replacement races with a concurrent metadata edit.** The replacement carries name/artist captured when it started, while metadata edits "must not be overwritten by a later transfer completion." The commit rule isn't defined. | Serialized sync table, source update modal | **R-MUT-5**: a replacement applies name/artist as a field-level patch, only for fields the user changed. Its file facts commit independently. If a changed field conflicts with a newer revision, the replacement moves to `needs_attention` and the user must reconfirm. |
| C-7 | **The delivery order is inverted** (see summary item 3). Account deletion with device purge on every platform is also scheduled in phase 1, before any device storage exists. | Delivery plan | New milestone order M0–M9 and a separate set of spikes. Server-side deletion lands in M1. Each client's purge lands with that client's milestone. |

## High-severity gaps

| ID | Gap | Resolution |
| --- | --- | --- |
| H-1 | Missing server entities. `/me/devices/{deviceID}` and the cleanup receipts reference an *installation*, but no `devices` table exists. Account deletion wipes "OAuth credentials," but no table stores them. The order representation for large drag-sorted lists is unspecified. | Added `devices`, `drive_connections` (pending D-7), and an explicit `position` key strategy (**R-ORG-6**). |
| H-2 | Overlapping tables: `change_log` and `sync_event_outbox`, plus `mutation_receipts` and `track_sync_operations.request_id`. It's also unclear what `import_jobs` is for, since local import is client-side. | One `account_events` log (an ordered per-user cursor that doubles as the outbox) with a `dispatch_state` column. Receipts are the single idempotency record and link to operations. `import_jobs` is limited to Drive imports, or dropped if Drive stays client-side. |
| H-3 | Devices are supposed to purge only one account's data, but the plan's `Songs` directory is shared, with no per-account layout. | **R-DEV-2**: each account gets its own namespace, `Songs/{accountId}/{contentId}`, with a separate SQLite/IndexedDB database per account. |
| H-4 | The plan doesn't say what happens when a device has been offline longer than the change feed retains events. | **R-EVT-4**: an expired cursor returns `cursor_expired`, and the client does a full snapshot resync. |
| H-5 | On Playlist and Group detail pages, the row **Delete** action is the destructive library-wide delete. There is no "Remove from this playlist/group". | Screen spec now has **Remove from playlist** / **Remove from group** row actions. Library-wide delete moves under "Delete from library…". |
| H-6 | Status names are scattered: `needs_source`, `needs_relink`, `needs_download`, `source_changed`, Cleanup pending, Not downloaded, Needs attention, Waiting…, Received/Staged/Applied. They mix account-level, cloud, and device dimensions. | New **State model** section with three independent dimensions plus operation state, and the UI label for each. |
| H-7 | The plan never defines the supported audio formats, the checksum algorithm, or the client-side metadata/duration extraction tooling, but the song limits and verification depend on all three. | **D-8** (format allowlist). Fixed SHA-256 (**R-ID-5**). Extraction tooling is part of spike S4. |
| H-8 | Offline or expired-session behavior is unspecified: whether a signed-out user can browse or play the cache, and what logout does to the web IndexedDB cache on a shared computer. | **D-9**. Proposed: an expired or revoked session keeps local playback of the cache until the user explicitly logs out. Explicit logout on web clears that account's IndexedDB/OPFS data. |
| H-9 | It's unclear where a Drive import runs. "Download to the device" conflicts with the "worker relays Drive bytes" egress text and the "Go workers: metadata extraction" stack row. Token storage is also unclear. | **D-7**. Proposed: Drive import is client-side only, with no server Drive tokens. The stack row was corrected. |

## Medium and low findings

| ID | Finding | Resolution |
| --- | --- | --- |
| M-1 | The online-only list includes "deleting artists," but there's no UI to rename, merge, or delete an artist, and no rule for orphaned artists. | Artist page gets rename and delete-when-unused actions. Merge stays out of scope (D-10). |
| M-2 | The API has search, but no screen offers it. | Added search to Songs, Library, and the pickers. |
| M-3 | Drag reordering conflicts with cursor-paginated lists. The position of a new import isn't defined. | R-ORG-6 (fractional keys, new imports appended, "Move to top/bottom" menu actions for long lists). |
| M-4 | Player gaps: Previous semantics, what turning shuffle off restores, whether the queue is a snapshot or live, and whether player state survives a restart. | **R-PLAY-6…9** (proposed defaults, listed in D-11). |
| M-5 | Account gaps: there's no email change or signed-in password change, and registration with an existing email can reveal that the account exists. | Generic registration response (R-AUTH-2). Email/password change goes to D-12. |
| M-6 | There's no per-account cloud storage quota, even though storage costs $0.015/GB-month and the Hobby plan is capped at 1 TB. | **D-6**. |
| M-7 | Several operational pieces are unspecified: the rate-limit store with multiple replicas, the email provider, observability, test strategy, CI, repo layout, and the API error envelope. | Engineering conventions § and milestone M0. Email provider is D-13. |
| M-8 | The order of Home preview rows is undefined. The Library filter can deselect every type. | Home shows the most recently added or updated items. The filter requires at least one type. |
| L-1 | Terminology: "datalake" names both the Songs page and the bucket, and **Resync** (replace source) collides with cloud sync. | Glossary. Renamed the actions **Replace audio file** and **Locate file on this device**. "Library" is the scope name. |
| L-2 | Checklist text refers to "item 5…9", but the bullets aren't numbered. | Rules and decisions have stable IDs. The checklist was replaced by the spike and decision tables. |
| L-3 | Pausing a scope uses `DELETE /scopes/{id}`, which reads as deletion. | `POST …/pause` and `POST …/resume`. |
| L-4 | Localization isn't mentioned (planned bucket regions exclude Brazil, so the users may be in Brazil). | D-14. |
| L-5 | Final publication re-uploads the verified bytes from the worker, which doubles the service egress for every synced song. | Noted as a cost in R-CLOUD-4. Conditional copy (spike S3) may remove it. |

## Contradictions found and how the restructured plan settles them

- The stack row "Go workers: … metadata extraction" contradicts client-side extraction for local-only songs. The server only *verifies cloud objects*, and the clients do the extraction.
- Phase 0 required "web fetches no cloud audio before selection" before cloud sync existed. That check moved to M5.
- Unsync says "preserve accepted … content reference," while the delete/replace text implies content is deleted. Settled: on unsync, the content row stays with no bytes anywhere (`cloud_state=none`, `source_mode=none`). It is deleted only when it's replaced or the track is deleted (R-SRC-4).
- On Group detail, the playlist row **Delete** "removes that playlist configuration," which deletes the playlist from every other group too. Settled by H-5.

## Rules confirmed unchanged

The restructuring kept every agreed decision, including:

- online-only mutations
- fresh content per explicit submission, with no duplicate detection
- one active operation per user/track
- playback pins
- explicit unsync with suppression
- 30 min / 128 MiB limits
- 30-day absolute sessions
- irreversible account deletion with device cleanup receipts
- manual organization only
- whole-song web playback
- bucket only as sync copy for installed clients

Only C-1, C-3, C-5, and C-6 add new rules. They are marked *proposed* in the plan until approved.

## Owner decisions, round 1 (2026-10-08)

The owner's feedback changed the data model. Plan v2 implements these decisions.

| Owner decision | Effect on the plan | Findings it resolves or replaces |
| --- | --- | --- |
| **Local songs are device-only.** The server stores nothing about them: no rows in `tracks` and no metadata. They are lost on reinstall. | New `R-OWN` rules. `tracks` holds synced songs only, and local songs live in per-account device SQLite. The v1 states `needs_source`/`needs_relink` on server documents, the "unsynced track document" and the "Locate file on this device" proposal are all removed. | Resolves **C-1** by design: no cross-device unsynced state exists. Removes the v1 rule that "local-only tracks still need an online catalog import" |
| **Playlists may contain local songs; other devices see only the synced ones.** | `R-ORG-4` **device overlay**: `local_playlist_items` on the device, merged with server memberships by a shared position-key space (`R-ORG-6`). When the song syncs, its overlay entries become server memberships in the same request | New design element. Ordering gap **M-3** is resolved through the shared key space |
| **Desktop is in scope** for web, mobile and desktop. | Milestone M4 is now *Desktop app + shared installed core*. The shell choice (D-1) is the only open part | Resolves **C-4** |
| **The web plays only cloud songs.** Adding a song on the web syncs it automatically, and "keep local" shows an install-the-app alert. | `R-OWN-5`, `R-CLOUD-2`, §7.2. Web OPFS, link mode and the copy-mode conflict are gone, and web offline downloads are deferred (D-5) | Resolves **C-3**; web storage is simplified to an IndexedDB catalog replica only |
| **Content order:** the track document is created first without content, then the content is uploaded and placed in local `Songs`, then the content ID is set. | `R-ID-3` with explicit pending tracks (`state=pending_upload`, `current_content_id` null). Proposed `R-ID-4` hides pending tracks on other devices and garbage-collects stalled ones (D-7) | Replaces the **C-2** "candidate content" proposal. Candidate content rows still exist only inside the operation for *replacements* |
| **Name/artist on replacement:** a field-by-field suggestion step (keep current or use new) comes before anything is applied. Only after confirmation are the file uploaded and the content ID changed. | `R-MUT-5` and flow §7.7. A concurrent edit from another device during the upload is shown again as a suggestion, never overwritten | Resolves **C-6** |

### Other fixes made in v2

- **Delivery order (C-7):** M0 foundations → M1 accounts → M2 mutation and event backbone → M3 web MVP (cloud-only, exercising every server contract) → M4 desktop with the shared installed core → M5 mobile → M6 Drive → M7 hardening. Mobile spikes start in M0.
- **Delete flow (C-5):** §7.8 covers local, synced and pending songs, plus remove-from-collection. Deletion takes the per-track slot. Other devices apply remote deletes under playback pins, and local deletes stop playback immediately.
- **Unsync:** redefined for the new model as §7.9 **Unsync (keep on this device)**. The server deletes the track, and the requesting installed device keeps the file as a local song with its memberships as overlay entries and auto-sync suppressed (D-4). The v1 "needs source" recovery flow no longer exists.
- **Simplifications enabled by the new model:**
  - The separate cloud manifest is merged into the catalog feed.
  - Server scope jobs are removed, because devices resolve their own local songs.
  - `import_jobs`/`drive_connections` are dropped (Drive import is client-side).
  - Downloads of immutable content no longer take the per-track slot (proposed, D-19).
- **New proposed rule:** local-song edits and overlay changes work offline because they touch no server data (D-18). Server data stays online-only.
- **Account deletion** now also purges the account's local songs from app storage, while linked originals stay untouched.
- **Earlier findings carried into v2:** H-1 (`devices` table), H-2 (single `account_events`), H-3 (per-account namespace), H-4 (`cursor_expired`), H-5 (remove vs delete), H-6 (state model), H-7 (SHA-256, formats D-8), H-8 (D-9), M-1 (artist rename/delete), M-2 (search), M-4 (player defaults D-11), M-5, M-6, M-7, M-8 and L-1 to L-4.

### Superseded in this file

The following parts of the tables above describe v1 and are kept only for history:

- the C-1 "Locate file on this device" proposal
- the C-2 candidate-content resolution, for initial sync
- the C-3 "web OPFS copy first" proposal
- the H-9 server-side Drive alternative

## Owner decisions, round 2 (2026-10-08)

| Decision | Owner's answer | Plan changes |
| --- | --- | --- |
| Terms (D-18) | "Local song" = "unsynced song": not on the server, not in `Songs`; its file is somewhere in device storage. "Synced song" = saved on the server and mirrored in `Songs` | Glossary rewritten; `Songs` holds only synced songs (and in-progress sync copies). The **Copy into Song Sleeve** import mode is removed: local import only records a locator (R-LOCAL-1). Local delete never deletes a file (§7.8). Purge removes records, not source files |
| D-4 Unsync | An unsynced song has its source somewhere in device storage, not in `Songs` | §7.9 rewritten: reuse the original file if it still matches, otherwise save a verified copy to a user-chosen folder, then remove from the server and from `Songs`. `synced_files.source_locator` added. Open detail: the default folder (`Music/Song Sleeve` proposed) |
| D-7 Visibility | New songs and new sources appear on other devices only when finished; progress only on the requesting device | R-ID-4 now covers replacements too: other devices keep the current content with no "updating" state until the switch |
| D-18 Local changes | Stay on the device, never seen by the server, lost on reinstall | R-OWN-2 and R-LOCAL-2 approved |
| D-5 Web | Nothing offline or local on the web; it only reads files the user picks to add a song or give a synced song a new source | New rule **R-WEB**: no IndexedDB, OPFS, Cache Storage or service worker; catalog and player state in memory only; in-progress uploads listed by the server via `origin_device_id`. Stack, §5.2, §7.2, §7.14, §9.2, M2 and the M3 exit tests updated |
| Drive on installed apps | Follows from the terms above | §7.13: downloaded Drive files are saved to a user-chosen device folder and imported as local songs |
| D-19 | Explanation requested | Still proposed. R-SYNC-1 now states how an unlocked download stays safe: install only if the content is still current, and keep retired objects until their download grants expire |

## Owner decisions, round 3 (2026-10-08)

| Decision | Owner's answer | Plan changes |
| --- | --- | --- |
| D-19 | Approved | R-SYNC-1 and the decisions table updated |
| `Songs` location | A `Song Sleeve` folder at the device root | Glossary, R-OWN-3, §9.3–§9.5 and S1/S5 updated. Android uses a SAF tree grant (fallback `Music/Song Sleeve`); iOS uses the app's `Documents` folder shown in Files. Staging and temp files stay in app-private storage |
| Folder check | The folder may be edited by hand, so it must stay an exact mirror of the server: restore missing songs, remove extras. Run on every app open, from a manual Settings trigger, and before every source change of a synced song | New rule R-DEV-4 and flow §7.15 (quick and full modes, rename detection, offline behavior, pinned-file and in-progress exceptions). §7.7 waits for the check. §7.10 step 4 runs it. Settings screen added. M4 exit tests added |
| Drive | Drive songs are always synced. Installed apps: download → `Songs` → upload. Web: straight to the cloud | §7.13 rewritten; §7.1 hands Drive picks to §7.13; M6 and its tests updated |
| Follow-on | Unsync copies and local sources can't be inside `Song Sleeve`, or the check would remove them | R-LOCAL-4 and §7.9 refuse such locations; the default unsync save folder is now the device's `Music` folder |
| New proposals | — | D-20 mirror file names; D-21 removed extras go to the system trash where one exists |

## Owner decisions, round 4 (2026-10-08)

| Decision | Owner's answer | Plan changes |
| --- | --- | --- |
| D-4 Unsync | There is no unsync. A synced song can only be deleted | The unsync flow is removed, and with it `reason=unsync`, the `unsync` operation kind, `auto_sync_suppressed`, the `suppressed` sync state, the scope modal's suppressed list, `synced_files.source_locator`, and saving files to user-chosen folders (from §9.3, S1, S5 and M4). R-LOCAL-5 now says syncing is one-way. The Songs menu offers **Sync to cloud** (local) and **Delete song** only. Flows §7.10–§7.15 are renumbered §7.9–§7.14; section numbers in the earlier rounds above refer to the old numbering |
| Sync copies the file | Copy only; duplication is not a concern | R-CLOUD-4 states that syncing always copies the source into `Songs`, never moves it, never offers to delete the original, and accepts the duplicate space |

## Owner decisions, round 5 (2026-10-13)

| Decision | Owner's answer | Plan changes |
| --- | --- | --- |
| D-1 Desktop shell | Try Wails, with Electron as the fallback | §4.1, §9.4, S5 and M4 updated; Tauri dropped. S5 now lists Windows and Linux checks (WebView2/WebKitGTK codecs, SMTC/MPRIS, libsecret, packaging), plus a fallback rule |
| Desktop systems | Windows and Linux for now; no Mac to test on | §1.2, §9.4, infrastructure and M4 exit tests updated; macOS deferred. Open detail: Linux package formats (AppImage and `.deb` proposed) |
| Linux packages | Both AppImage and `.deb` | §9.4, §10.2, D-1, M4 and its exit tests updated: AppImage updates itself; `.deb` updates go through the package manager, and the app only notifies |

## Owner decisions, round 6 (2026-10-13)

| Decision | Owner's answer | Plan changes |
| --- | --- | --- |
| Google Drive | Removed. Songs are added only from local files | Drive flow, the M6 Drive milestone, its tests and references removed; Hardening is now M6. Scope table lists cloud imports as out of scope. Flows after §7.11 renumbered (sessions §7.12, folder check §7.13). The `drive` anchor stays on §1.2 for older links |
| D-7 Sync visibility (reverses round 2) | Sync progress shows on every device. A sync works like a transaction: if it fails, the track document is deleted | R-ID-4 rewritten: progress on every device through non-durable SSE hints; all-or-nothing sync with a `track_sync_failed` event; failures are a rejected file, a cancel or delete from any device, a stall past the timeout, or a non-retryable error. The 7-day GC is replaced by a stall timeout (proposed 30 minutes). A failed sync returns the song to local with **Try again**. Interpretation: a failed **replacement** never deletes the song; it keeps the old content. §3, §5, §6, §7.2, §7.3, §7.7, §7.8, M3 and its exit tests updated |
| Stall timeout | 5 minutes | R-ID-4, D-7 and M3 tests updated. Multipart parts are sized to finish well within the timeout, and progress counts parts that reach the bucket. S2 must prove background uploads stay within it |
| Failed replacement | The old content is removed only after the new upload finishes, so a song is never left without audio | R-ID-4 and R-ID-5 state it explicitly: the old content stays current and stored until the switch transaction; a failure discards only the new upload |
| D-9 Logout | Option C: keep by default, with an explicit erase option | §7.12: **Log out** keeps the namespace; **Log out and remove from this device** deletes that account's folder, database and caches after a warning, as a resumable local purge. Profile and M4 exit tests updated |
| Session lifetime | 3 months instead of 30 days | R-AUTH-3 now 90 days (7,776,000 s), still absolute with no sliding renewal; §1.2, §5.1, Login screen, M1 and its exit tests updated |
| D-11 Player defaults | All four proposals approved | R-PLAY-5…8 no longer marked proposed; D-11 recorded as decided |
| D-20 File names | ID only | Mirror files are `{contentId}.{ext}` and are never renamed; the metadata-rename step was removed from the folder check (§7.13, §9.3) |
| D-21 Extra files | Delete permanently | The folder check deletes extras with no trash; R-DEV-4, §7.13, the Settings warning and M4 tests updated |
| D-2 Job queue | River approved | §4.1 stack rows and D-2 recorded as decided; the deletion sweep and operation slot stay independent of River |
| D-3 Audio backup | No backup of song audio at this early stage | §10 states the accepted risk; D-3 recorded as decided (revisit before a wider release); the second copy removed from M6. Database backups (D-16) are unaffected |
| D-6 Storage quota | 25 GB per account, by usage | New rule R-LIM-5: usage = current content plus in-progress reservations; reserved atomically when a sync or replacement starts, released on failure; `storage_quota_exceeded`; Profile usage and a 90% warning. `users` columns, `/me/summary`, M3 and its exit tests updated |
| D-8 Formats | Only MP3, AAC, FLAC and WAV | D-8 recorded as decided; the import step and the worker check (R-CLOUD-5) identify the real codec, so ALAC inside `.m4a` is rejected |
| D-10 Artists | No merge page; a smart search while choosing the artist finds similar names | R-ORG-2 gains the smart artist search (accent, punctuation, "The", "&/and" and typo tolerant, one matcher in `packages/core`), with **Did you mean…?** before creating a new artist; §7.1, D-10 and M3 tests updated |
| D-12 Account credentials | Password change in v2; email change is not a feature, and each email stays unique to one account | New rules R-AUTH-7 (permanent, unique email) and R-AUTH-8 (password change with the current password, other sessions revoked, notice email); `POST /me/password`, Profile and M1 tests updated |
| D-13 Email provider | Resend; the app uses its Vercel domain for now, with the domain setup left for later | D-13 decided. New §10.3 "Pending setup: custom domain" (DNS for Resend, API domain, cookies and CORS, build URLs); until then email uses Resend's test sender, which reaches only the owner. Because `vercel.app` → `railway.app` would need blocked third-party cookies, the web calls the API through a Vercel `/api` rewrite (§10.2, S6). Database operations moved to §10.4 |
| Email verification | Removed for now | R-AUTH-1 now: registration signs in immediately, no verification email; accepted risk that someone can claim an address they don't own. R-AUTH-2 changed: registration must reveal a taken email (`email_unavailable`), so it is rate-limited; password reset stays generic. `email_verification_tokens`, `email_verified_at` and the verify/resend endpoints removed; Login/Register screen and M1 tests updated; §10.3 lists bringing verification back as optional |
| D-14 Languages | English by default, with a Brazilian Portuguese option | Two locales `en` (default) and `pt-BR`; switch on Start/Login and on Profile, saved as `users.locale` via `PATCH /me`, applied on every device and to emails; M0, M1 and their tests updated |
| D-15 Event retention | 90 days approved | R-EVT-4 and D-15 updated; a scheduled job deletes older events |
| D-16 Database backups | Deferred to a future version | §10.4 now states the accepted risk and what the later work must cover (RPO/RTO, PITR, restore procedure, ledger replay, removing tracks whose audio is gone); restore drills removed from M6 and its tests; added to the out-of-scope table with D-3. The deletion ledger stays |
| D-17 Timing values | Approved as recommended | New R-SYNC-5 table: heartbeat 30 s, stall 5 min, upload grant 5 min, download grant 15 min, receipts 90 days; the separate lease is dropped. `mutation_receipts`, M3 and its tests updated |
| D-22 Mobile scope | Android only for now; no Mac or iPhone to test iOS | iOS removed from the platform table, data model, adapters (§9.3, §9.5), builds, spikes S1/S2/S4, M5 and its tests; added to the out-of-scope table. The code stays Capacitor-ready for iOS, and Apple references are kept for later. iPhone Safari is noted as untested |

## Final pre-implementation review (2026-10-13)

A full read of the plan after every owner decision through D-22. Nothing in the plan was changed by this review; each finding needs either an owner decision (**Decision**) or a plan fix that follows from rules already approved (**Fix**).

### Blocking: resolve before M0/M1

| ID | Gap | Why it matters | Recommendation | Type |
| --- | --- | --- | --- | --- |
| F-1 | Open registration with no email verification, a 25 GB quota per account and a Railway Hobby bucket capped at 1 TB | Anyone can create unlimited accounts and upload 25 GB each. About 40 full accounts fill the bucket, and the storage bill is yours | A registration switch (closed or invite allowlist) while the app is owner-only (it effectively is, since email reaches only the owner, §10.3). An alert at e.g. 70% of the bucket cap. A way for the operator to suspend an account | Decision |
| F-2 | Batch syncs (scope sync, multi-file add, web add) versus the 5-minute stall | If track documents are created for all queued songs at once, the ones still waiting behind the concurrency cap make no progress and fail after 5 minutes | Create a song's track document only when its upload is about to start. Queued songs stay local (`queued`) and invisible to the server. Fixed per-device concurrency (e.g. 2–3 uploads) | Fix |
| F-3 | Auto-sync retry loops | A file the worker rejects, or a full quota, returns the song to local. The next reconciliation auto-syncs it again: an endless upload, fail, retry loop that burns bandwidth and battery | A rejection marks the song **Needs attention** and excludes it from auto-sync until its file changes or the user retries. Transient failures back off. Quota full pauses auto-sync on the device with one notice until space is freed | Fix |
| F-4 | Folder check mass deletion | Extras are deleted permanently (D-21). If the replica is empty, stale or buggy (fresh reinstall, first start offline, a migration bug), every mirror file is "extra". With no audio backup (D-3), these mirrors may be the only other copy | Delete extras only after the replica has caught up in this session. Adopt files whose name is a known content ID and whose SHA-256 matches (no re-download after a reinstall). If a run would delete more than a threshold (e.g. 20 files or 10%), ask the user first | Fix (threshold: Decision) |
| F-5 | Client/API version compatibility | Desktop (`.deb` only notifies), Android and web builds will lag the server. Nothing defines what happens when an old client meets a changed API | An API version header and a minimum supported client version, with a `client_update_required` error and an **Update required** screen. Migrations stay backward-compatible for that window | Fix |
| F-6 | Full mirroring on phones | Every installed device downloads every synced song (up to 25 GB) into `Song Sleeve`, possibly over mobile data. The exact-mirror rule leaves no partial option | Approve full mirroring plus: transfers on Wi-Fi only by default on Android (setting to allow mobile data), and a low-storage rule (stop downloading, keep **Needs download**, one notice). The alternative (selective download) contradicts R-DEV-4 | Decision |

### High: resolve before the milestone named

| ID | Gap | Recommendation | Milestone | Type |
| --- | --- | --- | --- | --- |
| F-7 | Session transport for installed apps is unspecified: the web uses a cookie, but how do Wails and Capacitor send the token? Which origins does API CORS allow? | Login returns a bearer token for installed apps (`Authorization` header, OS secure store); the cookie is web-only, and CSRF applies only to cookie requests. List the Wails and Capacitor origins in API CORS | M1 | Fix |
| F-8 | No password policy or hashing choice | argon2id with stated parameters; a minimum length (e.g. 10); no maximum below 64; rate limits as already planned | M1 | Decision (minimum length) |
| F-9 | Legal and privacy: users upload music they may not own; pt-BR users imply Brazil's LGPD; data is stored outside Brazil; Google Play requires a privacy policy and data-safety form | Terms of use, privacy policy and a takedown contact before any external user; accept terms at registration. Pair it with the §10.3 pending setup | Before external users | Decision |
| F-10 | Sync edge cases around organization: a sync carries overlay memberships for a playlist deleted meanwhile; a local song links to a server artist that is renamed or deleted | The server drops memberships to missing playlists and reports them (it never rejects the sync). Local songs store the artist **name**; `artist_id` is only a cache, re-resolved through the smart matcher at sync | M3/M4 | Fix |
| F-11 | No relink flow, although `needs_relink` and a relink test exist. Playback of a `source_changed` song is undefined | **Relink:** pick the moved file; the same SHA-256 relinks silently and keeps the metadata, while a different hash becomes **Replace audio file** with the suggestion step. A `source_changed` song still plays but can't sync | M4 | Fix |
| F-12 | Parsing untrusted audio (ffprobe in the worker, metadata extractors in clients) | Run ffprobe in a separate process with a timeout, memory limit and no network. Cap the bytes client extractors read | M3 | Fix |
| F-13 | The web player's queue snapshot needs the full ordered list, but web lists are paginated | An endpoint returning the ordered track IDs of a list (library, playlist or artist) for queue snapshots | M3 | Fix |
| F-14 | No scale target | State supported sizes (e.g. 10,000 songs, 500 playlists per account) for snapshot paging, list virtualization, SQLite indexes and folder-check duration, and test at that size | M2 | Decision |
| F-15 | Spikes have no time box and some have no fallback | Time boxes per spike. Fallbacks: S6 fails → polling `catalog/changes`; S3 fails (browser multipart to the Railway bucket) → single PUT per song under 128 MiB, or another S3 provider; S4 memory fails → a lower web size cap | M0 | Fix |
| F-16 | Android specifics missing from S1/S2 | Android 11+ doesn't let SAF grant the storage root itself, so the user must create or select a `Song Sleeve` subfolder in the picker. Long uploads need Android 14+ user-initiated data-transfer jobs or a typed foreground service. Phones without Google Play services get no FCM and catch up on open only | M5 | Fix |
| F-17 | Signing keys and their custody: Windows code-signing certificate (yearly cost), AppImage update key, Android upload key | Name where each key lives (CI secrets plus an offline backup) and budget the Windows certificate | M4/M5 | Decision (budget) |
| F-18 | No operator tooling, although the quota is "adjustable per account" | A minimal admin CLI: set quota, suspend, delete, view usage | M3 | Fix |
| F-19 | No client crash or error reporting | Choose a provider (or self-hosted), with no song names or paths in reports | M4 | Decision |

### Medium and low

| ID | Gap | Recommendation | Type |
| --- | --- | --- | --- |
| F-20 | No field limits | Max lengths for song, artist, playlist and group names; Unicode NFC; no empty names; caps on playlists, groups and items | Fix |
| F-21 | R-CLOUD-3 runs after the track document exists, so a known checksum mismatch flashes **Syncing…** on every device and then disappears | Check the file's facts before `POST /me/tracks` (doc-first order still holds) | Fix |
| F-22 | The stall clock for a replacement waiting behind another operation is undefined | The stall clock starts when the operation becomes active | Fix |
| F-23 | Web-only users can't get their audio back: there's no download action, and no audio backup (D-3) | Offer **Download file** on the web (an explicit save, not app storage), or accept the gap | Decision |
| F-24 | **Log out and remove** or an account switch with syncs in progress leaves them to fail by stall | Cancel them on the server first, when online | Fix |
| F-25 | Web device identity: the web stores nothing, so what is its device ID? | Derive it on the server from the session | Fix |
| F-26 | Product gaps to confirm: only manual order (no sort by name, artist or date); no album or track number; no volume control on Playing; "newest first" on Home needs creation times, including for local songs | Confirm each is intentional for this release | Decision |

### Text inconsistencies to clean up (no decision needed)

- §10.1 says "PostgreSQL backups still cover the catalog", and §4.1 lists "configured backups", both contradicting D-16.
- §4.4 rate limits still list "resend", from the removed email verification.
- §10.3 steps are numbered 1, 2, 3, 5, 4.
- §7.11 mentions "exported files" (no export exists) and a restore rule, while restores are deferred.
- Version naming: the plan calls this release "v2" ("Out of scope (v2)", "Target systems (v2)", "In v1, downloads did take it"), while the owner calls it v1.
- §11.1's "Proposed default" and "Needed by" columns are now mostly empty.

### Owner decisions on the final review, round 1 (2026-10-13)

| Finding | Owner's answer | Plan changes |
| --- | --- | --- |
| F-1 Open registration and storage cost | Cloud sync is an "upcoming feature" in the first release, enabled only for the owner's account; everyone else uses local files only. It opens to all users later | New R-CLOUD-8 and D-23: `users.cloud_sync_enabled`, set at registration from `CLOUD_SYNC_ALLOWLIST`; the server rejects every sync request from other accounts with `cloud_sync_unavailable`, so they use no bucket storage; sync actions show **Coming soon**. Consequence: on the web (cloud-only) an account without sync has nothing to play or add, and is pointed to the app. §1.2, R-CLOUD-2, §7.1, §7.2, `users`, `/me`, error codes, Songs screen, M1 and M4 tests updated |
| F-2 Batch syncs versus the stall timeout | Syncs go through a queue with multiple workers, three to start | New R-CLOUD-9 and D-24: a per-device queue (persisted on installed apps, in memory on the web) with 3 workers; a track document is created only when a worker starts the song. §7.3 step 0, §7.2, §7.4, §7.9 and M3/M4 tests updated |
| F-3 Auto-sync retry loops | A failed sync is never retried automatically; it's marked failed and the user retries. In a batch, each song syncs on its own in the queue | D-25: `sync_failed` songs are skipped by auto-sync and scopes; **Try again** per song and **Retry failed syncs** per list; one failure never affects the rest of the batch. Resuming an interrupted part within a running sync still happens automatically. R-ID-4, §7.3, §7.4, §7.9, Songs screen and tests updated |
| Web in the first release (raised by the owner) | The web app is still built and is a milestone, but only the owner uses it, for testing. The public web presence is a landing page that presents the app and its features and points to the downloads; it is the only public web interaction in the first release | New R-WEB-ACCESS and R-SITE, D-26: web registration and sign-in are allowed only for allowlisted accounts (`web_app_unavailable` otherwise, no session), the web app is `noindex` and never linked; new §8.5 landing page in `apps/site` (features, "cloud sync coming soon", download buttons with OS detection, Terms/Privacy, en/pt-BR, static with no API calls or cookies). §1.2, R-CLOUD-8, §4.3, §7.2, §8.2, §10.2 (two Vercel projects), §10.3 domain layout, D-22, M1 (gate and test), M3 renamed "Web app (cloud-only, owner-only)", M6 adds the landing page and its test. Consequences: F-23 (web-only users can't get audio back) no longer applies, since no one but the owner uses the web; iPhone users have no client in this release. New open decisions: D-27 (where non-owners reset their password) and D-28 (where installers are hosted) |
| D-27 Password reset for non-owner accounts | A web page | §7.12: the reset page is a public route of the web app, the one exception to R-WEB-ACCESS; it sets the password only, creates no web session, and tells the user to log in in the app. M1 test updated |
| D-28 Installer hosting | GitHub Releases | §10.2: every installer attached to a GitHub Release with stable asset names and checksums; the auto-update channel and the `.deb` notice read the latest release; a separate public releases repository if this one stays private. §8.5: stable `latest/download` links, version and sizes baked in at build time so the landing page makes no runtime API calls |
| F-4 Folder-check mass-deletion safety | The owner proposed a manifest of the folder kept inside it, compared with the server so a reinstall that keeps the folder knows what's there, plus constant checks so manual edits are caught. Accepted with three refinements: the server's catalog is the reference (no per-device copy stored on the server), a lost manifest is rebuilt from the files instead of wiping the folder, and "constant" checking means a quick check on open, resume and (desktop) folder change, with full checks on demand | D-29, §7.13 rewritten: `.songsleeve-manifest.json` per account folder (content ID, name, size, SHA-256, sync time; atomic writes; a hint, never proof); quick and full modes; rebuild/adopt by content ID and checksum; extras deleted only after catching up with the server; confirmation above 20 files. R-DEV-4, §5.2 `synced_files` and the M4 folder-check tests updated (the old "offline: extras are removed" test now says nothing is deleted offline) |

### Fixes applied without a decision (2026-10-13)

Approved by the owner as a batch ("yes, do this").

| Finding | Plan changes |
| --- | --- |
| F-5 Client/API compatibility | §4.4: `X-Client-Platform`/`X-Client-Version` on every request, a minimum version per platform, `426 client_update_required` with the update action; installed apps keep local features; additive API changes, breaking ones behind a new route version. M1 build and test |
| F-7 Session transport | R-AUTH-3: cookie (web, CSRF) or bearer token (installed apps, secure store), bound to its transport; CORS only for exact web origins, since installed apps call the API and bucket from native code; a webview origin is added only if a spike needs it. M1 build and test |
| F-10 Sync edge cases | R-ORG-4: memberships to playlists deleted during a sync are dropped and returned as `dropped_playlist_ids`; local songs store the artist name with `artist_id` as a cache that follows renames and is re-resolved at sync. M4 test |
| F-11 Relink | New §7.7.1: same SHA-256 relinks silently; a different file continues as **Replace audio file**; a `source_changed` song still plays but can't sync, and **Use changed file** rereads it. M4 test |
| F-12 Untrusted media | §4.4: ffprobe in a separate process with a 30 s timeout, memory limit and no network; client extractors capped at 16 MiB of tags, off the UI thread. M3 build and test |
| F-13 Web queue snapshot | §6: `GET /me/queue-ids`; §8.4 uses it on the web. M3 build and test |
| F-15 Spikes | §11.2: time box and adopted fallback per spike (S1 5 d, S2 5 d, S3 3 d, S4 3 d, S5 5 d, S6 2 d) |
| F-16 Android specifics | §9.5: Android 11+ can't grant the storage root, so the user creates or selects a `Song Sleeve` folder in the picker; long transfers as user-initiated data transfer jobs (Android 14+) or a `dataSync` foreground service; no FCM without Google Play services. S1/S2 updated |
| F-18 Operator tooling | New §10.5: `server/cmd/admin` CLI (usage, quota, cloud sync toggle, suspend, deletion, stalled work), `users.suspended_at` with `account_suspended`, and an `admin_actions` audit table. M3 build and test |
| F-20 Field limits | §4.4: trimmed, NFC, no control characters, not empty; song and artist 200, playlist and group 100 characters; `invalid_field`. Count caps wait for D-33 (F-14). M1 test |
| F-21 Facts before the track document | §7.3 step 0: the worker re-checks size, SHA-256 and limits before `POST /me/tracks`; step 2 verifies the copy. M4 test |
| F-22 Stall clock | R-SYNC-2: a waiting operation has no stall clock; it starts when the operation becomes active |
| F-24 Syncs at logout or account switch | §7.12 and §7.10: when online, cancel this device's syncs and replacements on the server first (new `POST /me/operations/{id}/cancel`); plain logout or a switch returns them to `queued`, logout-and-remove drops them. M4 test |
| F-25 Web device identity | §5.1 `devices`: the server creates a `web` device at each web login and binds it to the session; `GET /me` returns `device_id` |
| F-23 Web download action | Closed: no one but the owner uses the web in this release (D-26) |

**Text cleanup done:** the backup wording in §4.1, §10.1 and §7.11 now matches D-16; "resend" removed from §4.4 rate limits; §10.3 steps renumbered; "exported files" removed; version naming now reads "plan revision 2" for the document and "v1 release" for the product; §11.1 reduced to ID, decision and milestone, with the open owner decisions added as D-30 to D-36.

**Still open:** D-30 (F-6), D-31 (F-8), D-32 (F-9), D-33 (F-14), D-34 (F-17), D-35 (F-19), D-36 (F-26), and whether an interrupted upload part within a running sync should keep resuming automatically (F-3 follow-up; current plan: yes).

### Owner decisions on the final review, round 2 (2026-10-13)

The owner accepted every recommendation, and chose to ship the Windows installer unsigned for now.

| Decision | Owner's answer | Plan changes |
| --- | --- | --- |
| D-30 (F-6) Android downloads | Accepted | §9.5: Wi-Fi only by default, a Settings toggle for mobile data, pause under 1 GB free with **Needs download**, full mirroring kept. Settings screen and M5 test |
| D-31 (F-8) Password policy | Accepted | New R-AUTH-9: 10–128 characters, no composition rules, the 10,000 most common refused, argon2id with stored parameters and rehash on login. `users`, Register screen |
| D-32 (F-9) Legal | Accepted | New R-AUTH-10 (terms checkbox, `terms_accepted_at`/`terms_version`, re-acceptance via `POST /me/terms`); §8.5 legal pages (rights to the music, takedown contact, LGPD, data outside Brazil, retention, deletion); §10.3 step 6; M6 build |
| D-33 (F-14) Scale target | Accepted | §4.4: 10,000 synced songs, 500 playlists, 100 groups, 5,000 songs per playlist, 10,000 local songs per device; caps with `limit_reached`; M2 test |
| D-34 (F-17) Signing | Windows unsigned for now | §10.2: unsigned installer with the SmartScreen note on the landing page; Microsoft Store or a paid certificate chosen in the M6 release checklist; keys in GitHub Actions secrets with an offline backup; Play App Signing. §9.4 S5 list, M4 and M6 updated |
| D-35 (F-19) Crash reporting | Accepted | §4.4: Sentry free tier for server, web and apps with a shared scrubber; **Send crash reports** in Settings (on by default); covered by the Privacy Policy |
| D-36 (F-26) Product gaps | Accepted | §1.2 out of scope: sorting, album/track number, in-app volume. `created_at` on tracks, playlists and groups, `added_at` on local songs, for Home's "newest first" |
| F-3 follow-up | Accepted | No change: an interrupted upload part keeps resuming automatically within a running sync until the 5-minute stall timeout |

With this round, every finding of the final review is resolved. No owner decisions remain open in §11.1; the spikes (§11.2) and the M0 confirmation of the repository layout and libraries are the only items left before implementation.

### Final consistency check (2026-10-13)

A last pass over the whole plan found only wording gaps, fixed directly: the `Songs` folder location in the glossary and §9.3 now matches the Android folder grant (§9.5); registration links to draft legal pages until they're published (R-AUTH-10); §7.9 downloads follow D-30 on Android; the web's Sentry setup stores nothing (R-WEB); M1 tests now cover the password policy and terms acceptance. Every internal anchor and relative link resolves.
