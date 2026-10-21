/**
 * One player controller, independent of routes (§8.4, §9.1). It owns the queue,
 * position, duration and modes, and drives exactly one platform engine. System
 * controls go through the same methods, so R-PLAY-4 applies to them too.
 */

export interface PlayableTrack {
  id: string;
  name: string;
  artist: string | null;
  durationMs: number;
  /** The content that would play now; changes when a replacement commits. */
  contentId: string | null;
  playable: boolean;
}

export interface PlaybackSource {
  trackId: string;
  contentId: string;
}

export interface EngineEvents {
  onTime(positionSec: number): void;
  onDuration(durationSec: number): void;
  onEnded(): void;
  onPlaying(playing: boolean): void;
  onError(error: string): void;
  onLoadProgress(loaded: number, total: number): void;
}

/** A platform playback engine: the web's whole-song Blob player, or a native one. */
export interface PlayerEngine {
  load(source: PlaybackSource, events: EngineEvents): Promise<void>;
  play(): Promise<void>;
  pause(): void;
  seek(sec: number): void;
  /** Stops, clears the source and releases the resource (R-PLAY-2). */
  unload(): void;
  setMetadata?(t: PlayableTrack | null): void;
}

export type PlayerStatus = 'empty' | 'loading' | 'playing' | 'paused' | 'ended' | 'error';

/** Where playback started; the player bar links back to it (§8.3). */
export interface QueueSource {
  kind: 'songs' | 'library' | 'home' | 'playlist' | 'artist' | 'group';
  id?: string;
  label: string;
}

/** The page a queue source links to. */
export function sourceHref(source: QueueSource | null): string {
  switch (source?.kind) {
    case 'library':
      return '/library';
    case 'home':
      return '/home';
    case 'playlist':
      return `/playlists/${source.id}`;
    case 'artist':
      return `/artists/${source.id}`;
    case 'group':
      return `/groups/${source.id}`;
    default:
      return '/songs';
  }
}

/** What installed apps save to restore the player after a restart, paused (R-PLAY-5…8). */
export interface PlayerSnapshot {
  v: 1;
  trackId: string;
  queue: string[];
  original: string[];
  source: QueueSource | null;
  shuffle: boolean;
  repeatOne: boolean;
  positionSec: number;
}

export interface PlayerState {
  status: PlayerStatus;
  queue: string[];
  index: number;
  source: QueueSource | null;
  shuffle: boolean;
  repeatOne: boolean;
  positionSec: number;
  durationSec: number;
  loadProgress: { loaded: number; total: number } | null;
  /** Pending-change notice for the playing song (R-PIN-1). */
  notice: 'updated' | 'removed' | null;
  error: string | null;
  current: PlayableTrack | null;
}

export type Resolver = (trackId: string) => PlayableTrack | null;

function shuffled<T>(items: T[]): T[] {
  const a = [...items];
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [a[i], a[j]] = [a[j]!, a[i]!];
  }
  return a;
}

export class PlayerController {
  private state: PlayerState = {
    status: 'empty',
    queue: [],
    index: -1,
    source: null,
    shuffle: false,
    repeatOne: false,
    positionSec: 0,
    durationSec: 0,
    loadProgress: null,
    notice: null,
    error: null,
    current: null,
  };
  private original: string[] = [];
  private listeners = new Set<(s: PlayerState) => void>();
  /** The pinned resource: what is loaded, regardless of later catalog changes. */
  private pinned: PlaybackSource | null = null;
  private pendingRemoval = false;
  private loadSeq = 0;

  constructor(
    private readonly engine: PlayerEngine,
    private readonly resolve: Resolver,
  ) {}

  getState = (): PlayerState => this.state;

  subscribe = (fn: (s: PlayerState) => void): (() => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  private set(next: Partial<PlayerState>) {
    this.state = { ...this.state, ...next };
    for (const l of this.listeners) l(this.state);
  }

  /** Starts playback of a list; the queue is a snapshot of it (R-PLAY-5…8). */
  async playList(trackIds: string[], startId: string | null, source: QueueSource, opts: { shuffle?: boolean } = {}): Promise<void> {
    if (trackIds.length === 0) return;
    this.original = [...trackIds];
    let queue = [...trackIds];
    let index = startId ? Math.max(0, queue.indexOf(startId)) : 0;
    const shuffle = opts.shuffle ?? this.state.shuffle;
    if (shuffle) {
      const first = startId ?? queue[Math.floor(Math.random() * queue.length)]!;
      queue = [first, ...shuffled(queue.filter((id) => id !== first))];
      index = 0;
    }
    this.set({ queue, source, shuffle });
    const start = this.firstPlayable(index, 1);
    if (start < 0) return;
    await this.goTo(start, true);
  }

  private firstPlayable(from: number, step: 1 | -1): number {
    for (let i = from; i >= 0 && i < this.state.queue.length; i += step) {
      if (this.resolve(this.state.queue[i]!)?.playable) return i;
    }
    return -1;
  }

  /** Switching songs releases the pin; repeat-one turns off (R-PIN-2, R-PLAY-4). */
  private async goTo(index: number, autoplay: boolean, keepRepeat = false): Promise<void> {
    const id = this.state.queue[index];
    const t = id ? this.resolve(id) : null;
    if (!t || !t.playable || !t.contentId) return;
    this.releasePin();
    const seq = ++this.loadSeq;
    this.pinned = { trackId: t.id, contentId: t.contentId };
    this.set({
      index,
      current: t,
      status: 'loading',
      positionSec: 0,
      durationSec: t.durationMs / 1000,
      loadProgress: null,
      notice: null,
      error: null,
      repeatOne: keepRepeat ? this.state.repeatOne : false,
    });
    this.engine.setMetadata?.(t);
    try {
      await this.engine.load(this.pinned, this.events(seq));
      if (seq !== this.loadSeq) return;
      this.set({ loadProgress: null });
      if (autoplay) await this.engine.play();
      else this.set({ status: 'paused' });
    } catch (e) {
      if (seq !== this.loadSeq) return;
      if ((e as Error).name === 'AbortError') return;
      this.set({ status: 'error', error: (e as Error).message || 'playback_failed', loadProgress: null });
    }
  }

  private events(seq: number): EngineEvents {
    const live = () => seq === this.loadSeq;
    return {
      onTime: (p) => live() && this.set({ positionSec: p }),
      onDuration: (d) => live() && Number.isFinite(d) && d > 0 && this.set({ durationSec: d }),
      onEnded: () => live() && void this.handleEnded(),
      onPlaying: (playing) => live() && this.set({ status: playing ? 'playing' : this.state.status === 'loading' ? 'loading' : 'paused' }),
      onError: (err) => live() && this.set({ status: 'error', error: err }),
      onLoadProgress: (loaded, total) => live() && this.set({ loadProgress: { loaded, total } }),
    };
  }

  private releasePin() {
    this.pinned = null;
    if (this.pendingRemoval) {
      this.pendingRemoval = false;
    }
  }

  /** The song ending naturally is a pin boundary (R-PIN-2). */
  private async handleEnded(): Promise<void> {
    const cur = this.state.current;
    const removed = this.pendingRemoval;
    const updated = this.state.notice === 'updated';
    if (this.state.repeatOne && cur && !removed) {
      // The boundary is processed before the song repeats: a pending replacement
      // loads now (R-PIN-2).
      if (updated) return this.goTo(this.state.index, true, true);
      this.engine.seek(0);
      await this.engine.play();
      return;
    }
    const next = this.firstPlayable(this.state.index + 1, 1);
    if (next >= 0) return this.goTo(next, true);
    this.releasePin();
    if (removed) return this.stop();
    this.set({ status: 'ended', positionSec: this.state.durationSec });
  }

  async toggle(): Promise<void> {
    const s = this.state.status;
    if (s === 'playing') {
      this.engine.pause();
    } else if (s === 'paused') {
      await this.engine.play();
    } else if (s === 'ended' || s === 'error') {
      await this.goTo(this.state.index, true);
    }
  }

  play(): Promise<void> {
    return this.state.status === 'playing' ? Promise.resolve() : this.toggle();
  }

  pause(): void {
    if (this.state.status === 'playing') this.engine.pause();
  }

  async next(): Promise<void> {
    const i = this.firstPlayable(this.state.index + 1, 1);
    if (i >= 0) await this.goTo(i, true);
  }

  /** Previous restarts the song after 3 s, otherwise goes back (R-PLAY-5…8). */
  async previous(): Promise<void> {
    if (this.state.positionSec > 3 && this.state.status !== 'loading') {
      this.seek(0);
      return;
    }
    const i = this.firstPlayable(this.state.index - 1, -1);
    if (i >= 0) await this.goTo(i, true);
    else this.seek(0);
  }

  seek(sec: number): void {
    if (this.state.status === 'empty' || this.state.status === 'loading') return;
    const clamped = Math.max(0, Math.min(sec, this.state.durationSec || sec));
    this.engine.seek(clamped);
    this.set({ positionSec: clamped });
  }

  /** Shuffle keeps the current song in place; off restores the original order (R-PLAY-5…8). */
  setShuffle(on: boolean): void {
    if (on === this.state.shuffle) return;
    const curId = this.state.queue[this.state.index];
    if (on) {
      const rest = this.state.queue.filter((_, i) => i !== this.state.index);
      const queue = curId ? [curId, ...shuffled(rest)] : shuffled(rest);
      this.set({ shuffle: true, queue, index: curId ? 0 : -1 });
    } else {
      const queue = this.original.filter((id) => this.state.queue.includes(id));
      this.set({ shuffle: false, queue, index: curId ? queue.indexOf(curId) : -1 });
    }
  }

  toggleRepeat(): void {
    if (this.state.status === 'empty') return;
    this.set({ repeatOne: !this.state.repeatOne });
  }

  /** The state to save, or null when nothing is loaded. */
  snapshot(): PlayerSnapshot | null {
    const s = this.state;
    const trackId = s.queue[s.index];
    if (s.status === 'empty' || !trackId) return null;
    return {
      v: 1,
      trackId,
      queue: [...s.queue],
      original: [...this.original],
      source: s.source,
      shuffle: s.shuffle,
      repeatOne: s.repeatOne,
      positionSec: s.status === 'ended' ? 0 : Math.floor(s.positionSec),
    };
  }

  /**
   * Restores a saved state, paused at the saved position (R-PLAY-5…8). Songs that
   * no longer exist are dropped; nothing happens if the player is already in use
   * or the saved song is gone.
   */
  async restore(snap: PlayerSnapshot): Promise<boolean> {
    if (snap?.v !== 1 || this.state.status !== 'empty') return false;
    const exists = (id: string) => !!this.resolve(id);
    const queue = snap.queue.filter(exists);
    const index = queue.indexOf(snap.trackId);
    if (index < 0 || !this.resolve(snap.trackId)?.playable) return false;
    this.original = snap.original.filter(exists);
    this.set({ queue, source: snap.source, shuffle: snap.shuffle, repeatOne: snap.repeatOne });
    await this.goTo(index, false, true);
    const after: PlayerState = this.state; // goTo changed it
    if (after.status !== 'paused' || after.queue[after.index] !== snap.trackId) return false;
    if (snap.positionSec > 0) this.seek(Math.min(snap.positionSec, Math.max(0, this.state.durationSec - 1)));
    return true;
  }

  /** A voluntary stop releases the pin and the resource. */
  stop(): void {
    this.loadSeq++;
    this.releasePin();
    this.engine.unload();
    this.engine.setMetadata?.(null);
    this.set({ status: 'empty', current: null, index: -1, positionSec: 0, durationSec: 0, notice: null, loadProgress: null, error: null, repeatOne: false });
  }

  /** Cancels a whole-song download in progress (§9.2). */
  cancelLoad(): void {
    if (this.state.status === 'loading') this.stop();
  }

  hasPrevious(): boolean {
    return this.firstPlayable(this.state.index - 1, -1) >= 0;
  }

  hasNext(): boolean {
    return this.firstPlayable(this.state.index + 1, 1) >= 0;
  }

  /**
   * A catalog change for a track (R-PIN-1…3). If the playing song's content
   * changed or it was deleted elsewhere, keep playing the pinned resource and
   * show the notice; the change applies when the pin is released.
   */
  onTrackUpdated(trackId: string): void {
    const cur = this.state.current;
    if (!cur || cur.id !== trackId) return;
    const t = this.resolve(trackId);
    if (!t) return;
    const contentChanged = !!this.pinned && t.contentId !== this.pinned.contentId;
    this.set({
      current: { ...t, contentId: this.pinned?.contentId ?? t.contentId },
      notice: contentChanged && this.state.status !== 'ended' ? 'updated' : this.state.notice,
    });
  }

  onTrackDeleted(trackId: string, deletedHere: boolean): void {
    const cur = this.state.current;
    if (cur?.id !== trackId) return;
    if (deletedHere || this.state.status === 'loading' || this.state.status === 'ended' || this.state.status === 'error') {
      // Deleting the current song on this device stops playback at once (R-PIN-5).
      const queue = this.state.queue.filter((id) => id !== trackId);
      this.original = this.original.filter((id) => id !== trackId);
      this.stop();
      this.set({ queue });
      return;
    }
    this.pendingRemoval = true;
    this.set({ notice: 'removed' });
  }
}
