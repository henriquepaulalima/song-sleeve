# Whole-song browser playback and resource limits

> **Current decision (2026-10-07):** the user has approved a per-song maximum of 30 minutes AND 128 MiB across all clients/sources. Both limits are inclusive. Proposal-only wording below is the original research snapshot; use the [approved policy](../../../implementation-plan.html#approved-song-limits) for current enforcement. Implementation tests remain pending.

Research checked 2026-10-07. This note addresses transient whole-file playback; the sibling storage note covers persistent OPFS and quotas. Numerical thresholds below are proposals and calculations, not accepted application requirements or measured browser guarantees.

## Can common browsers load and retain a complete song?

### Takeaway

Current Chrome, Edge, Firefox, and Safari have the APIs required to fetch one complete encoded song, retain it as a Blob, and play its object URL. There is no defensible universal per-file limit or guaranteed safe size shared by those browsers; retaining a Blob does not necessarily mean every byte remains in physical RAM.

### Cited Findings

- Mozilla's browser compatibility data lists Blob support in Chrome, Edge, Firefox, Safari and their Android/iOS counterparts. The object-URL API is likewise supported across those browsers. These support entries establish API availability, not successful operation at a particular file size. — [Blob compatibility data](https://raw.githubusercontent.com/mdn/browser-compat-data/main/api/Blob.json); [Object URL compatibility data](https://raw.githubusercontent.com/mdn/browser-compat-data/main/api/URL.json).
- `Response.blob()` reads a response body to completion and yields a Blob; it is a broadly available API. An opaque cross-origin response produces an unusable empty Blob, so bucket CORS must allow the client to read the actual response. — [Mozilla Response.blob documentation](https://developer.mozilla.org/en-US/docs/Web/API/Response/blob).
- Chromium's Blob design describes blobs as potentially gigabytes in size and pages Blob data to disk when memory budgets are full or a blob is too large. Its documented in-memory pool budgets include 2 GB on certain desktop configurations and physical RAM / 100 on Android; disk budgets and free-space constraints also apply. These are internal implementation budgets, not web-platform per-file admission guarantees. Its design also warns about transient renderer memory growth while data is transferred and about retained Blob references. — [Chromium Blob storage design](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/storage/browser/blob/README.md).
- The File API specifies a Blob's size as an unsigned long long and an immutable byte sequence. That numeric representation does not promise successful allocation or a particular browser memory budget. — [W3C File API, Blob interface](https://w3c.github.io/FileAPI/#blob-section).
- A WebKit engineer explains that iOS web-content processes are subject to OS memory limits and that several different allocation limits coexist; device and memory pressure matter. Historical example numbers in this 2024 bug discussion are not a current application budget or universal safe limit. — [WebKit memory-limit discussion, comment 10](https://bugs.webkit.org/show_bug.cgi?id=268816#c10).
- Microsoft documents tab freezing/discarding under memory pressure even with sleeping tabs disabled. Playing sound affects heuristics but is not a hard memory-retention guarantee. — [Microsoft Edge sleeping-tabs policy](https://learn.microsoft.com/en-ie/deployedge/microsoft-edge-browser-policies/sleepingtabsenabled).

### Inferences

| Browser/platform | Single 50 MB encoded Blob | Single 100 MB encoded Blob | Single 300 MB encoded Blob |
| --- | --- | --- | --- |
| Chrome desktop | Required APIs available; resource-dependent | Required APIs available; resource-dependent | Required APIs available; larger unvalidated memory/disk and loading cost |
| Edge desktop | Required APIs available; resource-dependent | Required APIs available; resource-dependent | Required APIs available; larger unvalidated memory/disk and loading cost |
| Firefox desktop | Required APIs available; resource-dependent | Required APIs available; resource-dependent | Required APIs available; larger unvalidated memory/disk and loading cost |
| Safari macOS | Required APIs available; resource-dependent | Required APIs available; resource-dependent | Required APIs available; larger unvalidated memory/disk and loading cost |
| Chrome/Edge on Android | Required APIs available; real-device testing needed | Required APIs available; real-device testing needed | Required APIs available; avoid promising success on lower-resource devices |
| Firefox on Android | Required APIs available; real-device testing needed | Required APIs available; real-device testing needed | Required APIs available; avoid promising success on lower-resource devices |
| Safari on iOS/iPadOS | Required APIs available; real-device testing needed | Required APIs available; real-device testing needed | Required APIs available; avoid promising success on lower-resource devices |
| Chrome/Edge/Firefox on iOS | Validate actual engine/OS configuration; brand alone is insufficient | Validate actual engine/OS configuration; brand alone is insufficient | No universal safe playback guarantee |

The table is a capability inference from API support and resource constraints; it is not the result of loading those files on each browser. An unsupported API, failed allocation, browser process termination, unsupported codec, and insufficient OPFS quota are different failure classes. None of the named browsers can honestly be labeled unable to store a 100 MB file solely from its brand, and none can be promised safe at that size on every device.

The persisted origin quota handled by OPFS/IndexedDB is not a playable-Blob RAM budget. Conversely, a temporary Blob may use browser-managed disk without being a durable offline library file. The app should describe the file as retained for current playback rather than promise that it lives exclusively in RAM.

### Gaps

- No primary cross-browser contract was found establishing a maximum safe 50/100/300 MB fetched audio Blob on all hardware. Device/browser tests remain necessary.
- Chrome internal budgets can change, and the design README is not a compatibility promise for Edge or every Chromium distribution.
- Exact behavior of iOS browser brands with alternative engine entitlements must be feature-tested instead of assuming their desktop engines.

## How should whole-song playback avoid unnecessary resource use?

### Takeaway

Use an HTML audio element to decode the already-downloaded encoded file as needed. Holding the encoded song is affordable compared with turning the entire track into an uncompressed Web Audio buffer; do not perform full-song `decodeAudioData()` merely to play or inspect duration.

### Cited Findings

- A Web Audio `AudioBuffer` is a memory-resident asset of 32-bit float PCM channels, intended for short sounds. The specification recommends an audio element/MediaElementAudioSourceNode for longer music. An audio element backed by a completed Blob preserves this app's full-download-before-play rule; that recommendation need not introduce cloud streaming. — [W3C Web Audio AudioBuffer specification](https://www.w3.org/TR/webaudio/#AudioBuffer).
- `decodeAudioData()` takes complete file data and returns a decoded AudioBuffer resampled to the context sample rate. — [Mozilla decodeAudioData documentation](https://developer.mozilla.org/en-US/docs/Web/API/BaseAudioContext/decodeAudioData).
- Active object URLs prevent their underlying objects from being garbage collected. Revoke unused URLs and release Blob references at an appropriate time. — [Mozilla Blob URL memory management](https://developer.mozilla.org/en-US/docs/Web/URI/Reference/Schemes/blob).
- Fetch exposes the response body as a ReadableStream and methods that produce Blob/ArrayBuffer results. AbortController can cancel fetch and response consumption. — [Fetch Standard, Body mixin](https://fetch.spec.whatwg.org/#body-mixin); [Mozilla AbortController documentation](https://developer.mozilla.org/en-US/docs/Web/API/AbortController).
- `canPlayType()` answers unsupported/maybe/probably rather than guarantee that a particular file plays; Media Capabilities can additionally evaluate codec/profile/bitrate support, smoothness and power efficiency. — [Mozilla canPlayType documentation](https://developer.mozilla.org/en-US/docs/Web/API/HTMLMediaElement/canPlayType); [Mozilla Media Capabilities documentation](https://developer.mozilla.org/en-US/docs/Web/API/Media_Capabilities_API).

### Inferences

- Derived PCM allocation: `duration_seconds × sample_rate × channels × 4 bytes`. Stereo float32 at 44.1 kHz takes 105.84 MB for 5 minutes and 635.04 MB (605.62 MiB) for 30 minutes, before the encoded file, application, copies, decoder or other tabs. This is why an encoded 72 MB MP3 must not be converted wholesale into a playback AudioBuffer.
- Proposed implementation safeguards: confirm metadata byte size/duration against an application limit before fetching; verify the response belongs to the expected accepted content version; count actual bytes while consuming the response and abort if the cap is exceeded, including when Content-Length is absent/wrong. Streams here are transport/progress/cap enforcement: do not start playback until the whole encoded file finishes.
- Build one bounded Blob without also retaining a complete ArrayBuffer, Base64 data URL, full decoded PCM or duplicated chunk array. Avoid cloning/teeing the response into separate simultaneous consumers. Processing that needs an incremental checksum should use incremental bounded work rather than an extra full-file buffer; exact memory behavior still needs profiling.
- On a user-requested track switch, stop/detach old audio, revoke its object URL, release the old Blob and cancel any obsolete fetch before starting the next whole-song load. A generation/content-version check prevents an old in-flight response becoming the new current song. Browser cleanup timing is not synchronous, so one Blob in app state does not guarantee precisely one encoded-file allocation at every instant.
- Preserve the existing no-interruption rule for server updates: keep the currently playing accepted file until EOF/explicit stop/switch; show the update notice and fetch the replacement only after releasing the old file. No next-track preload, simultaneous replacement Blob, gapless crossfade or progressive cloud playback is implied.
- Explicit OPFS offline storage remains separate. Do not add every one-track fetch to a service-worker persistent cache automatically. If offline storage is unavailable, playback can remain transient where Blob APIs/codec work.
- Show transfer progress, cancel, retries/errors, and the fact that playback starts after the full file arrives. Low space/memory/quota failures should offer the installed app/local file path instead of silently switching to a cloud-streaming feature.

### Gaps

- API availability does not determine peak working-set size. Full network consumption, Blob creation, decoder initialization, seek, repeated switches and server-update deferment require profiling on low-resource Android and iPhone devices.
- Release/revoke does not guarantee immediate physical-memory reclamation. The implementation needs repeated-switch soak tests, not only a single successful load.

## What size/duration policy and codec coverage are defensible?

### Takeaway

A proposed web playback envelope of both at most 30 minutes and at most 128 MiB (134,217,728 bytes) covers long compressed tracks, while filtering out large uncompressed/high-resolution files that undermine whole-file loading. It is a product/test target, not a sourced browser hard limit or a guarantee of success.

### Cited Findings

- MP3 is supported by mainstream Chrome/Edge/Firefox/Safari; FLAC also has broad modern support. AAC on Firefox can depend on platform decoding support, and codec support depends on container/profile. — [Mozilla audio codec guide](https://developer.mozilla.org/en-US/docs/Web/Media/Guides/Formats/Audio_codecs).
- WAV is a container; it can hold several codecs. Linear PCM is the usual interoperable payload, whereas support for other WAV codecs is sparse. — [Mozilla WAVE container guide](https://developer.mozilla.org/en-US/docs/Web/Media/Guides/Formats/Containers#wave_wav).
- Apple's older iOS guide lists MP3, AAC-LC/HE-AAC and uncompressed WAV/AIF; it is historical baseline evidence, not a current exhaustive compatibility table. — [Apple iOS HTML audio considerations](https://developer.apple.com/library/archive/documentation/AudioVideo/Conceptual/Using_HTML5_Audio_Video/Device-SpecificConsiderations/Device-SpecificConsiderations.html).
- WebKit's Safari 17.4 release adds full WebM support on iOS/iPadOS and Vorbis support there. This contradicts stale blanket statements that Safari cannot play Vorbis/WebM; current container/version data and runtime checks should take precedence. — [WebKit Safari 17.4 release notes](https://webkit.org/blog/15063/webkit-features-in-safari-17-4/#additional-codecs).
- A fixed WebKit FLAC-in-MP4 regression illustrates why exact container/codec strings matter even when a codec is broadly supported. — [WebKit FLAC-in-MP4 regression](https://bugs.webkit.org/show_bug.cgi?id=260491).

### Inferences

- Suggested initial web compatibility baseline: MP3; AAC-LC in M4A/MP4 with runtime platform checks; PCM WAV within size/duration limits; native-container FLAC within the same limits and tested across supported browsers. Other combinations should use feature checks plus actual decode/playback error handling. Do not claim all `.wav`, `.m4a` or `.flac` files are valid just from extensions, or automatically transcode personal files without a separate feature decision.
- `30 minutes AND 128 MiB` accommodates 30-minute 320 kbps audio: 72 MB / 68.66 MiB before embedded metadata/artwork. 64 MiB would exclude that case at roughly 27.96 minutes even before overhead. Neither envelope accommodates a 30-minute CD PCM WAV (317.52 MB / 302.81 MiB); 5-minute 24-bit/96 kHz stereo WAV is 172.8 MB / 164.79 MiB. These are calculated payload sizes, not measured files; embedded metadata/container and variable bit rate alter exact sizes.
- Use both limits, not one: duration limits bound extended playback/validation scenarios, while byte limits address high-bit-rate short tracks. A longer low-bit-rate track may still fit 128 MiB but fail the proposed duration rule; if users need DJ mixes/concerts, duration policy can be expanded independently after testing.
- Distinguish a web playback cap from an account import/upload cap. Native/local and cloud storage may reasonably allow larger files with native playback; globally rejecting them is a separate product choice. Web can display their metadata and an installed-app recommendation if whole-file web playback exceeds its supported envelope.
- No arbitrary mobile soft warning threshold was established by primary sources. The application can show actual estimated transfer time/data cost and a large-file confirmation, then choose a lower mobile warning or cap from device tests. A browser brand alone should not select an alleged safe memory limit.

Calculated ideal download times (`encoded_bytes × 8 / effective_bits_per_second`), excluding protocol overhead, latency, retries, signing and decoder startup:

| Complete encoded file | At 2 Mbps | At 10 Mbps | At 50 Mbps |
| --- | --- | --- | --- |
| 12 MB: 5-minute 320 kbps payload | 48 seconds | 9.6 seconds | 1.9 seconds |
| 72 MB: 30-minute 320 kbps payload | 4 minutes 48 seconds | 57.6 seconds | 11.5 seconds |
| 128 MiB proposed upper bound | 8 minutes 57 seconds | 1 minute 47 seconds | 21.5 seconds |
| 317.52 MB: 30-minute CD PCM WAV payload | 21 minutes 10 seconds | 4 minutes 14 seconds | 50.8 seconds |

Whole-file startup delay can be the stronger user-experience constraint even if a browser has sufficient storage. These calculations explain why metadata-only sync plus one selected full track is useful but is not instant playback on slower networks.

### Gaps

- The user has not selected a numerical product limit yet. Record the proposal/research and retain topic 8 testing/threshold selection as open.
- Duration and encoded-byte metadata for malformed/untrusted files need implementation validation and bounded parsing. Runtime codec probing is only a hint, not full file validation.
- Codec guide tables include stale details for some Safari/Chromium combinations; do not copy them as an exhaustive current format matrix. Validate representative actual fixtures and use vendor release notes/runtime support checks.
