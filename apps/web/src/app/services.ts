/**
 * The web app's application services, outside React (§4.3: screens call services,
 * never platform APIs). Everything here lives in memory only (R-WEB).
 */
import {
  ApiClient,
  CatalogStore,
  CatalogSyncService,
  PlayerController,
  isPlayable,
  type ConnectionState,
  type OperationDoc,
  type PlayableTrack,
} from '@song-sleeve/core';
import { WebAudioEngine } from '../player/webEngine';
import { Signal } from './signal';

export { Signal };
import { UploadQueue, type UploadFacts } from '../upload/uploader';

declare const __APP_VERSION__: string;
export const APP_VERSION = typeof __APP_VERSION__ === 'string' ? __APP_VERSION__ : '0.1.0';


export const unauthenticated = new Signal(0);
export const updateRequired = new Signal<boolean>(false);
export const connection = new Signal<ConnectionState>('connecting');
export const online = new Signal<boolean>(typeof navigator === 'undefined' ? true : navigator.onLine);

if (typeof window !== 'undefined') {
  window.addEventListener('online', () => online.set(true));
  window.addEventListener('offline', () => online.set(false));
}

export const api = new ApiClient({
  baseUrl: '/api',
  platform: 'web',
  version: APP_VERSION,
  onUnauthenticated: () => unauthenticated.set(unauthenticated.get() + 1),
  onUpdateRequired: () => updateRequired.set(true),
});

export const catalog = new CatalogStore();

function resolveTrack(id: string): PlayableTrack | null {
  const s = catalog.getState();
  const t = s.tracks.get(id);
  if (!t) return null;
  return {
    id: t.id,
    name: t.name,
    artist: t.artist_id ? (s.artists.get(t.artist_id)?.name ?? null) : null,
    durationMs: t.duration_ms,
    contentId: t.current_content_id,
    playable: isPlayable(t),
  };
}

export const engine = new WebAudioEngine(api);
export const player = new PlayerController(engine, resolveTrack);
export const uploads = new UploadQueue(api, catalog);

/** Track IDs this tab deleted itself, so the player stops at once for them (R-PIN-5). */
const deletedHere = new Set<string>();
export function markDeletedHere(trackId: string) {
  deletedHere.add(trackId);
  player.onTrackDeleted(trackId, true);
}

catalog.onTrackChange((id, change) => {
  if (change.kind === 'deleted') {
    player.onTrackDeleted(id, deletedHere.delete(id));
  } else {
    player.onTrackUpdated(id);
  }
});

catalog.onSyncFailure((f) => uploads.onTrackGone(f.trackId, f.reason));

// System media controls route through the one controller (§9.1).
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

let sync: CatalogSyncService | null = null;
let deviceId: string | null = null;

export function startSync(myDeviceId: string, hooks: { onAccountChanged: () => void; onAccountDeleted: () => void }): Promise<void> {
  stopSync();
  deviceId = myDeviceId;
  sync = new CatalogSyncService(api, catalog, {
    deviceId: () => deviceId,
    onAccountChanged: hooks.onAccountChanged,
    onAccountDeleted: hooks.onAccountDeleted,
    onConnectionChange: (s) => connection.set(s),
  });
  return sync.start();
}

export function stopSync() {
  sync?.stop();
  sync = null;
}

export function catchUp(): Promise<void> {
  return sync?.catchUp() ?? Promise.resolve();
}

/** Drops all in-memory state on logout or deletion (§7.12: the web has nothing stored). */
export function resetAll() {
  stopSync();
  player.stop();
  uploads.clear();
  replacements.clear();
  catalog.reset();
  api.setCsrfToken(null);
  api.outstanding.clear();
}

/**
 * Replacement uploads started in this tab (§7.7). A queued replacement waits for
 * the slot; this polls until it becomes active, then uploads.
 */
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

/** Warn before the tab closes while uploads run (§7.2). */
if (typeof window !== 'undefined') {
  window.addEventListener('beforeunload', (e) => {
    if (uploads.activeCount > 0 || replacements.count > 0) {
      e.preventDefault();
      e.returnValue = '';
    }
  });
}
