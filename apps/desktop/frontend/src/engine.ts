/**
 * The desktop playback engine (§9.4): the audio element plays from the shell's
 * loopback media server, which serves local songs from their source file, synced
 * songs from the Songs folder, and streams the rest. The shell pins the playing
 * track so accepted changes wait for the playback boundary (R-PIN-1).
 */
import type { EngineEvents, PlayableTrack, PlaybackSource, PlayerEngine } from '@song-sleeve/core';
import { local } from './bridge';
import { missingFormats, type AudioFormat } from './codecs';

export class DesktopAudioEngine implements PlayerEngine {
  private audio = new Audio();
  private events: EngineEvents | null = null;
  private loaded = false;
  // Pin changes go out in order: a release must never overtake the next pin.
  private pinQueue: Promise<unknown> = Promise.resolve();

  /** formatOf reads the song's format from the catalog, to name a missing codec. */
  constructor(private readonly formatOf: (trackId: string) => AudioFormat | undefined = () => undefined) {
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
      if (this.loaded) this.events?.onError(this.audio.error?.message || 'decode_error');
    });
  }

  private pin(trackId: string | null): Promise<{ url?: string }> {
    const next = this.pinQueue.then(() => local<{ url?: string }>('POST', '/play', { track_id: trackId }));
    this.pinQueue = next.catch(() => undefined);
    return next;
  }

  async load(source: PlaybackSource, events: EngineEvents): Promise<void> {
    this.reset();
    this.events = events;
    const { url } = await this.pin(source.trackId);
    if (!url) throw new Error('unavailable');
    if (this.events !== events) throw new DOMException('Aborted', 'AbortError');
    this.loaded = true;
    this.audio.src = url;
    await new Promise<void>((resolve, reject) => {
      const ok = () => {
        cleanup();
        resolve();
      };
      const fail = () => {
        cleanup();
        // "Not supported" also covers unreadable files; blame the codec only when
        // this system has no decoder for the song's format.
        const format = this.formatOf(source.trackId);
        reject(new Error(format && missingFormats().includes(format) ? 'unsupported_codec' : 'decode_error'));
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
    if (this.loaded) await this.audio.play();
  }

  pause(): void {
    this.audio.pause();
  }

  seek(sec: number): void {
    if (this.loaded) this.audio.currentTime = sec;
  }

  private reset() {
    this.events = null;
    this.loaded = false;
    this.audio.pause();
    this.audio.removeAttribute('src');
    this.audio.load();
  }

  unload(): void {
    this.reset();
    void this.pin(null).catch(() => undefined);
    if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'none';
  }

  /** System media controls (MPRIS through WebKitGTK, SMTC through WebView2; §9.4). */
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
