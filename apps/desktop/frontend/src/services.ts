/**
 * The desktop app's services: the same exports as the web's app/services.ts, which
 * this module replaces in the desktop build. The catalog comes from the shell's
 * SQLite replica merged with local songs, and the shell keeps it current (the
 * listener runs in Go, also while the window is hidden; §9.4). Server calls still
 * go through /api, which the shell forwards with the stored token.
 */
import { ApiClient, CatalogStore, PlayerController, type ConnectionState, type OperationDoc, type PlayableTrack, type PlayerSnapshot, type Snapshot } from '@song-sleeve/core';
import { UploadQueue, type UploadFacts } from '@web/upload/uploader';
import { local, onShell, shellFetch, type ShellState } from './bridge';
import { DesktopAudioEngine } from './engine';
import { Signal } from '@web/app/signal';

export { Signal };

declare const __APP_VERSION__: string;
export const APP_VERSION = typeof __APP_VERSION__ === 'string' ? __APP_VERSION__ : '0.1.0';


export const unauthenticated = new Signal(0);
export const updateRequired = new Signal<boolean>(false);
export const connection = new Signal<ConnectionState>('connecting');
/** Local work never needs a connection; server changes report it per request. */
export const online = new Signal<boolean>(true);
export const shellState = new Signal<ShellState | null>(null);

export const api = new ApiClient({
  baseUrl: '/api',
  platform: 'desktop',
  version: APP_VERSION,
  fetch: shellFetch,
  // The shell answers connection_required itself when the server is unreachable.
  isOnline: () => true,
  onUnauthenticated: () => unauthenticated.set(unauthenticated.get() + 1),
  onUpdateRequired: () => updateRequired.set(true),
});

export const catalog = new CatalogStore();

function resolveTrack(id: string): PlayableTrack | null {
  const s = catalog.getState();
  const t = s.tracks.get(id);
  if (!t) return null;
  const artist = t.artist_id ? (s.artists.get(t.artist_id)?.name ?? null) : null;
  if (t.device?.local) {
    // A local song's content is its file; a new file (Update source) is a new content.
    return { id: t.id, name: t.name, artist, durationMs: t.duration_ms, contentId: 'local:' + t.sha256, playable: t.device.health === 'ready' };
  }
  return { id: t.id, name: t.name, artist, durationMs: t.duration_ms, contentId: t.current_content_id, playable: t.state === 'ready' && !!t.current_content_id };
}

export const engine = new DesktopAudioEngine();
export const player = new PlayerController(engine, resolveTrack);
/** Web-style uploads aren't used on the desktop (the shell syncs), but screens read the queue. */
export const uploads = new UploadQueue(api, catalog);

const deletedHere = new Set<string>();
export function markDeletedHere(trackId: string) {
  deletedHere.add(trackId);
  player.onTrackDeleted(trackId, true);
}

catalog.onTrackChange((id, change) => {
  if (change.kind === 'deleted') player.onTrackDeleted(id, deletedHere.delete(id));
  else player.onTrackUpdated(id);
});

if (typeof navigator !== 'undefined' && 'mediaSession' in navigator) {
  const ms = navigator.mediaSession;
  ms.setActionHandler('play', () => void player.play());
  ms.setActionHandler('pause', () => player.pause());
  ms.setActionHandler('previoustrack', () => void player.previous());
  ms.setActionHandler('nexttrack', () => void player.next());
  try {
    ms.setActionHandler('seekto', (d) => d.seekTime != null && player.seek(d.seekTime));
  } catch {
    /* unsupported */
  }
}

export async function refreshShellState(): Promise<ShellState> {
  const st = await local<ShellState>('GET', '/state');
  shellState.set(st);
  connection.set(st.online ? 'live' : 'offline');
  return st;
}

let loadSeq = 0;
async function loadCatalog(): Promise<void> {
  const seq = ++loadSeq;
  const doc = await local<Snapshot & { cursor: number }>('GET', '/catalog');
  if (seq !== loadSeq) return; // a newer load already started
  catalog.loadSnapshot([doc]);
}

/**
 * Player state survives restarts (R-PLAY-5…8): it's saved in the account folder
 * when the song, queue or modes change, when a paused song is seeked, and every
 * 5 s while playing, so even a killed process restores close to where it was.
 */
let playerReady = false;
let lastKey = '';
let lastPos = -1;
let lastSave = 0;

function savePlayer(force = false) {
  if (!playerReady) return;
  const snap = player.snapshot();
  const key = JSON.stringify(snap && { ...snap, positionSec: 0 });
  const pos = snap?.positionSec ?? 0;
  const playing = player.getState().status === 'playing';
  const now = Date.now();
  const due = force || key !== lastKey || (playing ? now - lastSave >= 5000 : pos !== lastPos);
  if (!due) return;
  lastKey = key;
  lastPos = pos;
  lastSave = now;
  void local('PUT', '/player', { snapshot: snap }).catch(() => undefined);
}

player.subscribe(() => savePlayer());
if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', () => savePlayer(true));
  document.addEventListener('visibilitychange', () => document.visibilityState === 'hidden' && savePlayer(true));
}

async function restorePlayer(): Promise<void> {
  if (playerReady) return;
  try {
    const { snapshot } = await local<{ snapshot: PlayerSnapshot | null }>('GET', '/player');
    if (snapshot) await player.restore(snapshot);
  } catch {
    /* nothing saved, or the saved song can't play: start empty */
  } finally {
    playerReady = true;
  }
}

let unsubscribe: (() => void)[] = [];

/**
 * Same contract as the web's startSync: loads the catalog and keeps it current,
 * here from the shell's events instead of the server stream.
 */
export function startSync(_deviceId: string, hooks: { onAccountChanged: () => void; onAccountDeleted: () => void }): Promise<void> {
  stopSync();
  let pending: ReturnType<typeof setTimeout> | null = null;
  const reload = () => {
    if (pending) return;
    pending = setTimeout(() => {
      pending = null;
      void loadCatalog().catch(() => undefined);
    }, 50);
  };
  unsubscribe = [
    onShell('catalog', reload),
    onShell('connection', (d) => connection.set((d as { online: boolean }).online ? 'live' : 'offline')),
    onShell('progress', (d) => {
      const p = d as { id: string; confirmed: number; total: number };
      catalog.setProgress(p.id, p.confirmed, p.total);
    }),
    onShell('account', () => {
      void refreshShellState().then((st) => {
        if (!st.signed_in) {
          // The shell ended the session (401, revoke-all, account deleted elsewhere).
          unauthenticated.set(unauthenticated.get() + 1);
          return;
        }
        hooks.onAccountChanged();
      });
    }),
  ];
  void refreshShellState().catch(() => undefined);
  return loadCatalog().then(() => void restorePlayer());
}

export function stopSync() {
  for (const u of unsubscribe) u();
  unsubscribe = [];
}

export function catchUp(): Promise<void> {
  return loadCatalog();
}

export function resetAll() {
  // Signing out keeps the saved player state for this account.
  playerReady = false;
  lastKey = '';
  stopSync();
  player.stop();
  uploads.clear();
  replacements.clear();
  catalog.reset();
  api.setCsrfToken(null);
  api.outstanding.clear();
}

/** Replacement uploads for synced songs (§7.7), as on the web. */
class ReplacementManager {
  private running = new Map<string, AbortController>();
  readonly progress = new Signal<ReadonlyMap<string, number>>(new Map());

  start(trackId: string, file: File, facts: UploadFacts, op: OperationDoc, onFail: (reason: string) => void) {
    this.running.get(trackId)?.abort();
    const abort = new AbortController();
    this.running.set(trackId, abort);
    void (async () => {
      try {
        let cur = op;
        while (cur.state === 'waiting' && !abort.signal.aborted) {
          await new Promise((r) => setTimeout(r, 3000));
          cur = (await api.get<{ operation: OperationDoc }>(`/me/operations/${op.id}`)).operation;
        }
        if (cur.state !== 'active') {
          if (cur.state !== 'completed') onFail(cur.failure_reason ?? 'cancelled');
          return;
        }
        await uploads.replace({ trackId, file, facts, op: cur }, (c) => this.setProgress(trackId, c), abort.signal);
      } catch (e) {
        if ((e as Error).name !== 'AbortError') {
          void api.mutate('POST', `/me/operations/${op.id}/cancel`).catch(() => undefined);
          onFail('upload_error');
        }
      } finally {
        this.running.delete(trackId);
        const p = new Map(this.progress.get());
        p.delete(trackId);
        this.progress.set(p);
      }
    })();
  }

  private setProgress(trackId: string, confirmed: number) {
    const p = new Map(this.progress.get());
    p.set(trackId, confirmed);
    this.progress.set(p);
  }

  get count(): number {
    return this.running.size;
  }

  clear() {
    for (const a of this.running.values()) a.abort();
    this.running.clear();
  }
}

export const replacements = new ReplacementManager();
