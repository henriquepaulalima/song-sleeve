/**
 * The web playback engine (R-PLAY-2, §9.2): request a read grant, fetch the whole
 * song into one Blob with one object URL, and start only after the fetch finishes.
 * Exactly one resource is held; switching cancels the fetch, stops, clears the
 * source and revokes the URL. No prefetch, no second Blob, no cache, no
 * ArrayBuffer copy and no decodeAudioData().
 */
import { MAX_FILE_BYTES, type ApiClient, type EngineEvents, type PlayableTrack, type PlaybackSource, type PlayerEngine } from '@song-sleeve/core';

interface Grant {
  url: string;
  byte_size: number;
}

export class WebAudioEngine implements PlayerEngine {
  private audio: HTMLAudioElement;
  private url: string | null = null;
  private abort: AbortController | null = null;
  private events: EngineEvents | null = null;

  constructor(private readonly api: ApiClient) {
    this.audio = new Audio();
    this.audio.preload = 'auto';
    this.audio.addEventListener('timeupdate', () => {
      this.events?.onTime(this.audio.currentTime);
      this.updatePosition();
    });
    this.audio.addEventListener('durationchange', () => this.events?.onDuration(this.audio.duration));
    this.audio.addEventListener('ended', () => this.events?.onEnded());
    this.audio.addEventListener('playing', () => this.events?.onPlaying(true));
    this.audio.addEventListener('pause', () => {
      if (!this.audio.ended) this.events?.onPlaying(false);
    });
    this.audio.addEventListener('error', () => {
      if (this.url) this.events?.onError(this.audio.error?.message || 'decode_error');
    });
  }

  async load(source: PlaybackSource, events: EngineEvents): Promise<void> {
    this.unload();
    this.events = events;
    const abort = new AbortController();
    this.abort = abort;
    const grant = await this.api.post<Grant>(`/me/content/${source.contentId}/download-grant`, undefined, { signal: abort.signal });
    const expected = grant.byte_size;
    const res = await fetch(grant.url, { signal: abort.signal, cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer' });
    if (!res.ok || !res.body) throw new Error('download_failed');
    // Count received bytes ourselves: cancel and clean up on overflow even if
    // Content-Length is missing or wrong (R-LIM-2).
    let loaded = 0;
    const limit = Math.min(expected, MAX_FILE_BYTES);
    const counter = new TransformStream<Uint8Array, Uint8Array>({
      transform: (chunk, ctl) => {
        loaded += chunk.byteLength;
        if (loaded > limit) {
          ctl.error(new Error('song_too_large'));
          abort.abort();
          return;
        }
        events.onLoadProgress(loaded, expected);
        ctl.enqueue(chunk);
      },
    });
    const blob = await new Response(res.body.pipeThrough(counter), { headers: { 'Content-Type': res.headers.get('Content-Type') ?? 'audio/mpeg' } }).blob();
    if (abort.signal.aborted) throw new DOMException('Aborted', 'AbortError');
    if (blob.size !== expected) throw new Error('download_incomplete');
    this.url = URL.createObjectURL(blob);
    this.abort = null;
    this.audio.src = this.url;
    await new Promise<void>((resolve, reject) => {
      const ok = () => {
        cleanup();
        resolve();
      };
      const fail = () => {
        cleanup();
        reject(new Error('decode_error'));
      };
      const cleanup = () => {
        this.audio.removeEventListener('loadedmetadata', ok);
        this.audio.removeEventListener('error', fail);
      };
      this.audio.addEventListener('loadedmetadata', ok);
      this.audio.addEventListener('error', fail);
    });
  }

  async play(): Promise<void> {
    if (!this.url) return;
    await this.audio.play();
  }

  pause(): void {
    this.audio.pause();
  }

  seek(sec: number): void {
    if (this.url) this.audio.currentTime = sec;
  }

  unload(): void {
    this.abort?.abort();
    this.abort = null;
    this.events = null;
    this.audio.pause();
    this.audio.removeAttribute('src');
    this.audio.load();
    if (this.url) URL.revokeObjectURL(this.url);
    this.url = null;
    if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'none';
  }

  /** Media Session metadata: the artist only when there is one; no artwork yet (§9.1). */
  setMetadata(t: PlayableTrack | null): void {
    if (!('mediaSession' in navigator)) return;
    navigator.mediaSession.metadata = t ? new MediaMetadata({ title: t.name, ...(t.artist ? { artist: t.artist } : {}) }) : null;
  }

  private updatePosition() {
    if (!('mediaSession' in navigator) || !Number.isFinite(this.audio.duration)) return;
    try {
      navigator.mediaSession.setPositionState({ duration: this.audio.duration, position: Math.min(this.audio.currentTime, this.audio.duration), playbackRate: 1 });
      navigator.mediaSession.playbackState = this.audio.paused ? 'paused' : 'playing';
    } catch {
      /* position state unsupported */
    }
  }
}
