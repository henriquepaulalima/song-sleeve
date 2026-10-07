# TypeScript clients and installed-platform capabilities

Research checked 2026-10-07 against the current implementation plan. Recommendations below are proposals; no platform/framework substitution has been approved. Required behavior remains local playback on installed clients, optional cloud mirrors, account-authoritative metadata, online-only edits, and web playback of one complete selected song capped at 30 minutes AND 128 MiB.

## Does React, TypeScript and Vite fit the web interface and shared application logic?

### Takeaway

Keep React + TypeScript + Vite. Add explicit routing, accepted-server-state handling, local persistence, and platform adapters; changing to a server-rendering framework would not remove the browser or native capability gaps.

### Cited Findings

- React documents Vite as a starting tool while emphasizing that a from-scratch SPA needs additional routing and data-fetching choices. React Router offers declarative, data, and framework modes, allowing client routing without adopting a second backend. — [React SPA guidance](https://react.dev/learn/build-a-react-app-from-scratch); [React Router modes](https://reactrouter.com/start/modes).
- TanStack Query supports mutation success/error handling and optional persisted offline mutations. Its default network mode can pause work and continue on reconnection; these conveniences require deliberate configuration for this app's no-replay policy. — [Mutations](https://tanstack.com/query/latest/docs/framework/react/guides/mutations); [Network modes](https://tanstack.com/query/latest/docs/framework/react/guides/network-mode).
- dnd-kit documents keyboard accessibility, screen-reader support and pointer/touch interaction. It is a candidate for manual library, playlist-song and group-playlist ordering, not automatic organization. — [dnd-kit](https://dndkit.com/).
- A normal browser file input exposes selected files rather than reusable filesystem authority. Persistent `showOpenFilePicker()` has limited cross-browser availability and requires HTTPS and user interaction. OPFS is origin-private, quota-limited storage whose files are not the user's ordinary visible folders. — [File input](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/input/file); [Persistent picker](https://developer.mozilla.org/en-US/docs/Web/API/Window/showOpenFilePicker); [OPFS](https://developer.mozilla.org/en-US/docs/Web/API/File_System_API/Origin_private_file_system).
- Dexie provides an IndexedDB abstraction with indexed stores and transactions. Browser Media Session exposes metadata and transport handlers where supported; it is distinct from native media-session services. — [Dexie](https://dexie.org/docs/Dexie/Dexie); [Media Session](https://developer.mozilla.org/en-US/docs/Web/API/Media_Session_API).

### Inferences

- Propose React Router for navigation, TanStack Query for API reads and accepted mutation results, and dnd-kit for accessible ordering. Keep player and reconciliation controllers above routes. Use explicit application services rather than triggering file transfers from list components.
- For user mutations, reject known disconnection immediately and still require the actual authenticated API result. If using TanStack mutations, set `networkMode: 'always'` and `retry: false`, do not persist/resume mutations, and avoid optimistic authoritative updates. A lost-response request ID may be persisted only for status recovery. Cached reads and already-local player controls can continue offline.
- Propose Dexie for accepted catalog/location metadata on web and OPFS for explicit controlled copies/offline downloads. An ordinary selected File may work for the current visit but is not a durable linked original after restart. Capability-detect persistent pickers and revalidate grants; offer copy mode or reselection when unavailable rather than promise all browsers support original-location links.
- Keep the specified whole-song web pipeline: fetch fully, enforce received-byte limits, create one Blob/object URL, then play through HTML audio. No prefetch, progressive playback, second active Blob or full-track decoded buffer. Metadata notifications must not eagerly load audio. Browser quotas are not a memory guarantee; use the existing browser-limit research rather than claim 128 MiB always plays.

### Gaps

- Test actual codecs, memory pressure, permission renewal, folder imports, resource release and service-worker/account-cache purge across the supported browser matrix. No framework makes an origin-private file durable against user clearing or eviction.
- Specify paginated/virtualized lists for large libraries and the interaction between sorting, drag ordering and unloaded pages; these are UI contracts, not solved by selecting a drag library.

## Can Capacitor support linked originals, background local playback and background synchronization?

### Takeaway

Capacitor remains viable, but only with real native integrations. Keep it provisionally for maximum React DOM reuse; budget Swift/Kotlin work or evaluated paid plugins, and validate storage, playback and synchronization before implementing the full screen set.

### Cited Findings

- Capacitor Filesystem supports sandbox directories and Android content URIs. Native binary `readFile` results are strings/Base64; `readFileInChunks` is native-only. `downloadFile` is deprecated in favor of File Transfer, which documents native file upload/download and progress. These APIs should not require transferring whole 128 MiB files through Base64. — [Filesystem](https://capacitorjs.com/docs/apis/filesystem); [File Transfer](https://capacitorjs.com/docs/apis/file-transfer).
- Android's Storage Access Framework supports file and tree selection, with `takePersistableUriPermission` for retained grants; moving/deleting the selected document can invalidate access. iOS directory selection uses security-scoped URLs/bookmarks, coordinated access, and explicit start/stop access; permissions remain revocable. — [Android document access](https://developer.android.com/training/data-storage/shared/documents-files); [Apple directory access](https://developer.apple.com/documentation/uikit/providing-access-to-directories?changes=l_5&language=objc).
- Capawesome File Picker exposes `pickFiles`/`pickDirectory`, with an iOS directory bookmark from version 8.1. Its paid File Manager documents persisted directory access, current-grant lookup/release, recursive operations and checksums. This verifies a candidate directory-grant API, not every individual-file linking scenario. — [File Picker](https://capawesome.io/docs/sdks/capacitor/file-picker/); [File Manager](https://capawesome.io/docs/sdks/capacitor/file-manager/).
- Android Media3 recommends hosting Player and MediaSession in a `MediaSessionService`, independent of the activity. Apple requires an appropriate playback audio session/background capability and provides system controls through MediaPlayer APIs. Capawesome's paid Audio Player documents file-URI playback, native background playlist advancement and media controls when the WebView is suspended. — [Media3 service](https://developer.android.com/media/media3/session/background-playback); [Apple audio session](https://developer.apple.com/documentation/AVFAudio/AVAudioSession?changes=_11); [Apple system controls](https://developer.apple.com/library/archive/documentation/AudioVideo/Conceptual/MediaPlaybackGuide/Contents/Resources/en.lproj/RefiningTheUserExperience/RefiningTheUserExperience.html); [Audio Player](https://capawesome.io/docs/sdks/capacitor/audio-player/).
- Official Capacitor Push Notifications explicitly does not handle iOS silent pushes. Killed Android apps need a native `FirebaseMessagingService` for data-only handling. Background Runner has bounded execution, approximately 30 seconds on iOS and inexact Android periodic intervals of at least 15 minutes. — [Push limitations](https://capacitorjs.com/docs/apis/push-notifications); [Background Runner limits](https://capacitorjs.com/docs/apis/background-runner).
- Android WorkManager provides persistent, constrained work; FCM normal-priority messages can be delayed in Doze. Apple's background-update guidance describes bounded wakeups rather than continuous execution, and background URLSession is the platform mechanism for background file transfers. — [WorkManager](https://developer.android.com/develop/background-work/background-tasks/persistent); [FCM priority](https://firebase.google.com/docs/cloud-messaging/android-message-priority); [Apple notification guide](https://developer.apple.com/library/archive/documentation/NetworkingInternet/Conceptual/RemoteNotificationsPG/CreatingtheNotificationPayload.html); [Apple downloads](https://developer.apple.com/documentation/foundation/downloading-files-in-the-background).
- Expo Audio supplies native background playback, lock-screen controls and a player that can live outside component lifetimes. Expo BackgroundTask still uses OS scheduling and stops after user termination; switching frameworks cannot guarantee immediate background sync. — [Expo Audio](https://docs.expo.dev/versions/latest/sdk/audio/); [Expo BackgroundTask](https://docs.expo.dev/versions/latest/sdk/background-task/).

### Inferences

- Define native `FileAccess`, `AudioPlayer`, `TransferScheduler`, `DeviceSync`, `LocalCatalog` and `CredentialStore` adapters. The mobile player accepts verified local file/URI sources only; its remote URL capability must never become a cloud-playback fallback.
- Keep app-controlled account-specific `Songs` under persistent app storage rather than disposable cache. Choose the storage/backup policy explicitly. Linked originals need persisted grants and revocation handling; release grants on deletion but never delete external files. Default copy/link choice remains the user's decision.
- Evaluate Capawesome File Picker + paid File Manager/Audio Player as optional implementation candidates, alongside custom native adapters. Check license/update access and costs before choosing. Directory permission APIs do not establish persisted per-file permission support; test selected-file paths, provider URIs and bookmarks end to end.
- Background synchronization still needs a native FCM/WorkManager pipeline on Android and APNs handler/background URLSession pipeline on iOS, sharing durable account-scoped reconciliation with foreground work. Native code must perform authenticated catch-up, receipt checks, file application and cleanup without React executing. Official File Transfer documentation alone does not establish process-independent background restoration.
- If native playback is the dominant concern and Capacitor spikes fail, propose React Native + Expo for mobile while keeping the React/Vite web app and Go API. This offers a documented native audio path but changes mobile UI components and still requires custom grant/background-transfer/lifecycle integration. It is an alternative, not a mandatory replacement.

### Gaps

- Real-device spikes must verify local-only playback after restart/lock, next-track advancement while the WebView is suspended, repeat-current reset, audio focus/interruption, EOF-triggered pending replacement/unsync and immediate deletion stop.
- Validate large-file checksums and duration without whole-file JS copies; Android `MediaMetadataRetriever` and Apple asynchronous AVAsset duration are candidate native metadata mechanisms, with codec/precision/error cases needing tests. — [Android metadata API](https://developer.android.com/reference/android/media/MediaMetadataRetriever); [Apple duration properties](https://developer.apple.com/documentation/avfoundation/avasset-async-properties).
- Test push delay/force-stop, retained grants, native/foreground SQLite concurrency, expiring server sessions and signed download grants, cancelled transfers and late callbacks. A resumed OS transfer must not bypass the server's per-track operation ownership or recreate audio after deletion. None of the evaluated frameworks supplies these application invariants automatically.

## What is missing for desktop packaging and durable device indexes?

### Takeaway

Desktop needs an explicit shell selection; a browser/PWA does not provide the specified desktop native filesystem behavior. Propose evaluating Wails first because it preserves React/TypeScript UI and introduces Go local services, with Tauri and Electron as justified alternatives.

### Cited Findings

- Wails supports Go plus web UI, React/TypeScript templates, native desktop dialogs, and system rendering engines on Windows/macOS/Linux. Its options distinguish hiding a window from terminating the process. Current v2 documentation identifies v3 as beta; verify a release baseline at implementation. — [Wails](https://wails.io/docs/introduction/); [Dialogs](https://wails.io/docs/reference/runtime/dialog/); [Window lifetime](https://wails.io/docs/reference/options/).
- Tauri uses system WebViews with a Rust core; official filesystem/SQL plugins expose scoped operations and SQLite support. This adds Rust/toolchain knowledge while preserving a TS frontend and the separate Go API. — [Tauri](https://v2.tauri.app/start/); [Filesystem](https://v2.tauri.app/plugin/file-system/); [SQL](https://v2.tauri.app/plugin/sql/).
- Electron provides a Node main process separate from Chromium renderers. Native dialog selection and narrow preload IPC bridges fit file access, but its security guidance requires renderer isolation and IPC validation. — [Process model](https://www.electronjs.org/docs/latest/tutorial/process-model); [Dialogs](https://www.electronjs.org/docs/latest/api/dialog); [Security](https://www.electronjs.org/docs/latest/tutorial/security).
- Capacitor Community SQLite documents native databases, transactions and account-app storage locations; current release evidence includes Capacitor 8 support. A JS plugin API does not alone establish native-background-worker access to the same database/coordinator. — [SQLite plugin](https://github.com/capacitor-community/sqlite/blob/master/README.md); [API](https://github.com/capacitor-community/sqlite/blob/master/docs/API.md); [Releases](https://github.com/capacitor-community/sqlite/releases).

### Inferences

- Wails is the best first desktop candidate for this user's Go preference; local Go services can own file enumeration, hashes, SQLite and event catch-up while minimized. Desktop Go is an installation component, separate from Railway's Gin API. Tauri prioritizes a small distribution with an additional language; Electron prioritizes a consistent Chromium/TS environment with a larger runtime. No unsupported memory/size benchmark is claimed.
- Whichever shell wins, host sync work outside window React lifetimes and expose narrow operations, not arbitrary filesystem commands. Test file URLs/local-serving rules, media keys, codecs, sleep/reconnect, signing/updates and macOS sandbox grants. Shell selection does not imply a built-in production music player.
- Keep device SQLite for installed accepted replicas and IndexedDB for web. Persist operation IDs/cursors, installed content IDs, grants, staging/application records and account-scoped cleanup checkpoints; never build an offline edit queue. Use one native coordinator for file/index changes and playback pins. Confirmed account deletion needs a checkpoint outside the account DB and cancellation before purge/acknowledgement.

### Gaps

- Desktop platform list, shell, media-control integration and distribution are unselected. Wails/Tauri system engines require codec validation; Electron's bundled engine still does not make every audio codec available.
- Validate native SQLite ownership/concurrency with worker execution, secure credential persistence accessible at the permitted background time, and physical deletion of per-account files without affecting another account or external originals. Browser and installed stores intentionally have different durability contracts.
