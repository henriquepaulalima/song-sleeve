# Song duration and audio-file size estimates

> **Current decision (2026-10-07):** the user has approved a per-song maximum of 30 minutes AND 128 MiB across all clients/sources. Both limits are inclusive. Proposal-only wording below is the original research snapshot; use the [approved policy](../../../implementation-plan.html#approved-song-limits) for current enforcement. Implementation tests remain pending.

Research date: 2026-10-07. Scope: inputs for checklist topic 8; all size/duration limits below are proposals, not approved product rules. This note does not assess browser quota guarantees or native codec matrices; sibling researchers cover those subjects.

## What duration should a personal music library expect?

### Takeaway
Use **3–5 minutes** as a practical baseline for ordinary song test fixtures, **10–30 minutes** for longer-song fixtures, and **60 minutes** as an extended stress case. These are engineering ranges, not a universal empirical average or a promise to accept every musical work.

### Cited Findings
- Chartmetric's original 2024 report says chart-appearing Spotify tracks were most likely between **2:51 and 3:09**. Its observed extremes include a 32-second track and a **73-minute mixtape**. This is a chart-selected dataset, not a representative sample of every user's library. — [Chartmetric Year in Music 2024](https://reports.chartmetric.com/2024/chartmetric-year-in-music-2024)
- Chartmetric's original analysis of discographies of 200 highly ranked country/pop/hip-hop artists found 2022 means around **3.2 minutes for hip-hop**, **3.36 for pop**, and **3.35 for country**. Genre, sampling and year affect duration. — [Chartmetric discography analysis](https://hmc.chartmetric.com/taylor-swift-eras-tour-shorter-songs-bigger-albums/)
- IFPI used a three-minute track as a conversion benchmark when explaining weekly listening hours. That illustrates a useful planning benchmark but is **not a measured catalog-wide mean**. — [IFPI Engaging with Music 2021 announcement](https://www.ifpi.org/ifpi-releases-engaging-with-music-2021/)

### Inferences
- A three-minute benchmark is supported for popular music; budgeting up to five minutes broadens the normal fixture without misrepresenting an empirical average.
- A ten-minute song may be a large WAV while a sixty-minute compressed mix can be smaller. Duration and encoded size therefore need separate checks.
- A 30-minute envelope covers considerably longer tracks than chart-pop baselines but does not cover all mixes/live recordings/classical works; do not claim universal musical coverage.

### Gaps
- No representative, genre-independent mean for a user's mixed downloaded library was established. The actual user library is the best input to a percentile analysis later.
- The user has not chosen the product's maximum duration or byte limit; retain topic 8 as open pending policy choice and real-device tests.

## How large are lossy, lossless and uncompressed songs?

### Takeaway
Ordinary **3–5-minute 128–320 kbps** songs have about **2.88–12 MB** of encoded audio; 30 minutes at 320 kbps is **72 MB / 68.66 MiB**. CD-quality WAV and high-resolution WAV are much larger, and FLAC has no guaranteed compression ratio.

### Cited Findings
- The MP3 co-inventor's technical paper describes MPEG-1 Layer III's bitrate range through **320 kbit/s**, and frame-level bitrate switching. — [Fraunhofer, MP3 and AAC Explained](https://www.iis.fraunhofer.de/content/dam/iis/de/doc/ame/conference/AES-17-Conference_mp3-and-AAC-explained_AES17.pdf)
- Apple's own current Digital Masters tools generate **256 kbps AAC** tracks. This supplies a concrete common high-quality lossy bitrate benchmark, not a universal AAC maximum. — [Apple Digital Masters](https://www.apple.com/apple-music/apple-digital-masters/)
- Library of Congress identifies **44.1 kHz** as standard CD sampling and **96 kHz** as a preservation recommendation; LPCM bit depth and sample rate determine uncompressed fidelity. — [Library of Congress PCM format](https://loc.gov/preservation/digital/formats/fdd/fdd000016.shtml)
- FLAC's standard describes typical CD audio as **two channels, 44.1 kHz, 16 bits**; FLAC losslessly reduces PCM storage with efficiency dependent on signal/block characteristics. — [RFC 9639](https://www.rfc-editor.org/rfc/rfc9639.html)
- Xiph's FAQ explicitly says FLAC does not have a requested fixed bitrate and may range from near-zero for silence to around the input rate for noise. Consequently an assumed 'FLAC is always half a WAV' is invalid. — [FLAC FAQ, achievable bitrate](https://www.xiph.org/flac/faq.html#general__lowest_bitrate)

### Inferences
The calculations below are exact audio-payload arithmetic for the stated constant/average bitrates, displayed with decimal MB to three places and binary MiB rounded to two places. They exclude file headers, frame-padding variation, metadata, embedded artwork and transport overhead. Check actual file byte length in implementation, never infer admission solely from filename/duration/nominal bitrate.

- **Lossy bytes = seconds × bitrate in bits/second ÷ 8**. A nominal variable-bitrate setting is only an estimate; actual average bitrate or byte count is required.
- **PCM bytes = seconds × samples/second × bits/sample × channels ÷ 8**.
- **1 MB = 1,000,000 bytes; 1 MiB = 1,048,576 bytes**. These units must be labeled consistently in the UI and policy.
- CD-quality stereo PCM = 44,100 × 16 × 2 = **1,411,200 bits/second**, or **10.584 MB/minute**.
- High-resolution stereo PCM 24-bit/96 kHz = 96,000 × 24 × 2 = **4,608,000 bits/second**, or **34.560 MB/minute**.
- At the same encoded bitrate and duration, MP3 and AAC have similar payload sizes; codec quality and playback support are separate questions.

| Duration | 128 kbps | 256 kbps | 320 kbps | WAV PCM 16-bit / 44.1 kHz stereo | WAV PCM 24-bit / 96 kHz stereo |
| --- | --- | --- | --- | --- | --- |
| 3 min | 2.880 MB / 2.75 MiB | 5.760 MB / 5.49 MiB | 7.200 MB / 6.87 MiB | 31.752 MB / 30.28 MiB | 103.680 MB / 98.88 MiB |
| 5 min | 4.800 MB / 4.58 MiB | 9.600 MB / 9.16 MiB | 12.000 MB / 11.44 MiB | 52.920 MB / 50.47 MiB | 172.800 MB / 164.79 MiB |
| 10 min | 9.600 MB / 9.16 MiB | 19.200 MB / 18.31 MiB | 24.000 MB / 22.89 MiB | 105.840 MB / 100.94 MiB | 345.600 MB / 329.59 MiB |
| 20 min | 19.200 MB / 18.31 MiB | 38.400 MB / 36.62 MiB | 48.000 MB / 45.78 MiB | 211.680 MB / 201.87 MiB | 691.200 MB / 659.18 MiB |
| 30 min | 28.800 MB / 27.47 MiB | 57.600 MB / 54.93 MiB | 72.000 MB / 68.66 MiB | 317.520 MB / 302.81 MiB | 1036.800 MB / 988.77 MiB |
| 60 min | 57.600 MB / 54.93 MiB | 115.200 MB / 109.86 MiB | 144.000 MB / 137.33 MiB | 635.040 MB / 605.62 MiB | 2073.600 MB / 1977.54 MiB |

For a concrete FLAC **illustration only**, if a 5-minute CD PCM track compressed to 60% of its audio payload, it would be 31.752 MB / 30.28 MiB before metadata; if a 30-minute one did, it would be 190.512 MB / 181.69 MiB. **60% is an assumed scenario, not a guaranteed or measured universal FLAC ratio.** Measure the actual file instead of using it as an acceptance rule.

### Gaps
- FLAC/ALAC size distributions depend on audio complexity, sample rate, bit depth, encoder and metadata. The listed PCM values are comparison baselines, not guaranteed FLAC bounds.
- A .wav extension does not imply the particular PCM profile in the table, and .m4a does not always imply AAC; inspect actual codec/container.

## What whole-file web envelope is a reasonable candidate?

### Takeaway
A proposed **30-minute AND 128 MiB** whole-file web limit would admit 30-minute 320 kbps songs and many larger ordinary tracks. **64 MiB** is a stricter candidate for mobile testing but cannot cover a full 30-minute 320 kbps song. Neither number is a browser-supported guaranteed RAM budget; choose only after browser findings and real-device measurements.

### Cited Findings
- The Web Audio standard specifies in-memory AudioBuffer as non-interleaved **32-bit floating point linear PCM**. Full decoding therefore expands compressed music independently of compressed file size. — [Web Audio AudioBuffer specification](https://webaudio.github.io/web-audio-api/#AudioBuffer)
- Long-form media need not be decoded into a full AudioBuffer: a media element can be connected to a Web Audio graph through MediaElementAudioSourceNode. — [Web Audio MediaElementAudioSourceNode specification](https://webaudio.github.io/web-audio-api/#MediaElementAudioSourceNode)

### Inferences
- Keep the existing selected-song whole-download design, playing a Blob/object URL through HTMLAudioElement; do **not** add full-song decodeAudioData/AudioBuffer as the playback implementation. Fully decoded 30-minute 44.1 kHz stereo float32 would be **635.040 MB / 605.62 MiB**, even when the compressed song was only 72 MB.
- **64 MiB = 67.109 MB**: around **27.96 minutes** of 320 kbps payload before embedded metadata, so a 30-minute song at that bitrate fails. It can contain a 5-minute CD WAV (52.92 MB), but not a 3-minute high-res WAV (103.68 MB). A 20-minute AND64 MiB mobile candidate covers 20-minute320kbps (48 MB) with payload headroom, but excludes some ordinary-duration high-res files.
- **128 MiB = 134.218 MB**: covers a 30-minute320kbps payload (72 MB), 10-minute CD WAV (105.84 MB) and 3-minute24/96 WAV (103.68 MB). It does not cover a 5-minute24/96 WAV (172.8 MB), 30-minute CD WAV (317.52 MB), or60-minute320kbps (144 MB). Thus byte and duration limits must both pass.
- Extending compressed-song support to **60 minutes at320kbps** needs at least **144 MB /137.33 MiB plus embedded metadata**, e.g. a proposed160 MiB limit, with much stricter tests and longer wait times. It should not be treated as default just because a disk quota can hold it.
- For72 MB, theoretical download waiting is28.8 seconds at20 Mbps or115.2 seconds at5 Mbps, before overhead, contention and server delay. Whole-file-first playback means these waits directly affect time to music.
- No compressed-file byte cap implies a memory cap. Browser response buffering, decoder buffers, GC timing and application copies can increase peak memory. A Blob may be disk-backed; the API does not promise this or exact residency. Browser storage quotas govern durable origin storage, not transient playback process memory.
- Candidate envelope is a **research decision**, not approved architecture policy. Keep checklist topic8open pending user choice and mobile/desktop tests. A file can fit persistent storage yet exceed app whole-file policy or fail decoding.

### Gaps
- Browser RAM limits are not a fixed transferable per-file guarantee. Actual device/browser builds must be tested with boundary files, rapid track switching, interruptions and memory pressure.
- Browser codec/container support is assessed in sibling research; this note does not claim every browser can decode every format/profile in the size table.
- Larger files may remain valid in installed clients while being unplayable under the chosen web envelope; the account/import policy and per-client playback policy must be decided separately.
