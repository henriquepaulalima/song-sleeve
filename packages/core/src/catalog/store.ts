/**
 * The in-memory catalog: accepted server data only (R-MUT-2). The web keeps it in
 * memory and refetches it after a reload (R-WEB); installed apps will back the same
 * shape with their SQLite replica. Applying a document twice is harmless: older
 * revisions are ignored (R-EVT-3).
 */
import type { Artist, Changes, Group, Playlist, Preferences, Scope, Snapshot, Track } from '../api/types';

export interface SyncProgress {
  confirmed: number;
  declared: number;
  at: number;
}

export interface SyncFailure {
  trackId: string;
  name: string;
  reason: string;
  requestId?: string;
  at: number;
}

export interface CatalogState {
  loaded: boolean;
  cursor: number;
  tracks: ReadonlyMap<string, Track>;
  artists: ReadonlyMap<string, Artist>;
  playlists: ReadonlyMap<string, Playlist>;
  groups: ReadonlyMap<string, Group>;
  preferences: Preferences;
  scopes: readonly Scope[];
  /** Progress hints per track (coalesced, not events). */
  progress: ReadonlyMap<string, SyncProgress>;
  version: number;
}

export type CatalogListener = (state: CatalogState) => void;

export interface TrackChange {
  kind: 'upserted' | 'deleted';
  before?: Track;
  after?: Track;
}

const emptyPrefs: Preferences = { songs: true, playlists: true, groups: true, revision: 0 };

export class CatalogStore {
  private state: CatalogState = {
    loaded: false,
    cursor: 0,
    tracks: new Map(),
    artists: new Map(),
    playlists: new Map(),
    groups: new Map(),
    preferences: emptyPrefs,
    scopes: [],
    progress: new Map(),
    version: 0,
  };
  private listeners = new Set<CatalogListener>();
  private trackListeners = new Set<(id: string, change: TrackChange) => void>();
  private failureListeners = new Set<(f: SyncFailure) => void>();

  getState = (): CatalogState => this.state;

  subscribe = (fn: CatalogListener): (() => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  /** Track-level changes, for the player's pins (R-PIN-1). */
  onTrackChange(fn: (id: string, change: TrackChange) => void): () => void {
    this.trackListeners.add(fn);
    return () => this.trackListeners.delete(fn);
  }

  /** track_sync_failed events for syncs started on this device (R-ID-4). */
  onSyncFailure(fn: (f: SyncFailure) => void): () => void {
    this.failureListeners.add(fn);
    return () => this.failureListeners.delete(fn);
  }

  private commit(next: Partial<CatalogState>): void {
    this.state = { ...this.state, ...next, version: this.state.version + 1 };
    for (const l of this.listeners) l(this.state);
  }

  reset(): void {
    this.commit({
      loaded: false,
      cursor: 0,
      tracks: new Map(),
      artists: new Map(),
      playlists: new Map(),
      groups: new Map(),
      preferences: emptyPrefs,
      scopes: [],
      progress: new Map(),
    });
  }

  /** Replaces everything with a full snapshot (all pages merged). */
  loadSnapshot(pages: Snapshot[]): void {
    const first = pages[0];
    if (!first) return;
    const before = this.state.tracks;
    const tracks = new Map<string, Track>();
    for (const p of pages) for (const t of p.tracks) tracks.set(t.id, t);
    this.commit({
      loaded: true,
      cursor: first.cursor ?? 0,
      tracks,
      artists: new Map((first.artists ?? []).map((a) => [a.id, a])),
      playlists: new Map((first.playlists ?? []).map((p) => [p.id, p])),
      groups: new Map((first.groups ?? []).map((g) => [g.id, g])),
      preferences: first.preferences ?? emptyPrefs,
      scopes: first.scopes ?? [],
    });
    for (const [id, t] of before) {
      const after = tracks.get(id);
      if (!after) this.emitTrack(id, { kind: 'deleted', before: t });
      else if (after !== t) this.emitTrack(id, { kind: 'upserted', before: t, after });
    }
  }

  private emitTrack(id: string, change: TrackChange): void {
    for (const l of this.trackListeners) l(id, change);
  }

  /** Applies one page of the change feed. */
  applyChanges(ch: Changes, myDeviceId: string | null): void {
    const next: Partial<CatalogState> = { cursor: Math.max(this.state.cursor, ch.cursor) };
    const trackChanges: [string, TrackChange][] = [];
    if (ch.tracks?.length || ch.deleted.track?.length) {
      const tracks = new Map(this.state.tracks);
      for (const t of ch.tracks ?? []) {
        const cur = tracks.get(t.id);
        if (!cur || t.revision >= cur.revision) {
          tracks.set(t.id, t);
          trackChanges.push([t.id, { kind: 'upserted', before: cur, after: t }]);
        }
      }
      for (const id of ch.deleted.track ?? []) {
        const cur = tracks.get(id);
        if (cur) {
          tracks.delete(id);
          trackChanges.push([id, { kind: 'deleted', before: cur }]);
        }
      }
      next.tracks = tracks;
    }
    if (ch.artists?.length || ch.deleted.artist?.length) {
      next.artists = mergeDocs(this.state.artists, ch.artists ?? [], ch.deleted.artist ?? []);
    }
    if (ch.playlists?.length || ch.deleted.playlist?.length) {
      next.playlists = mergeDocs(this.state.playlists, ch.playlists ?? [], ch.deleted.playlist ?? []);
    }
    if (ch.groups?.length || ch.deleted.group?.length) {
      next.groups = mergeDocs(this.state.groups, ch.groups ?? [], ch.deleted.group ?? []);
    }
    if (ch.preferences && ch.preferences.revision >= this.state.preferences.revision) next.preferences = ch.preferences;
    if (ch.scopes) next.scopes = ch.scopes;
    // Progress hints are superseded by authoritative documents.
    if (next.tracks) {
      const progress = new Map(this.state.progress);
      for (const [id] of trackChanges) progress.delete(id);
      next.progress = progress;
    }
    this.commit(next);
    for (const [id, c] of trackChanges) this.emitTrack(id, c);
    for (const e of ch.events) {
      if (e.event_type !== 'track_sync_failed') continue;
      const p = e.payload ?? {};
      if (myDeviceId && p.origin_device_id !== myDeviceId) continue;
      const before = trackChanges.find(([id]) => id === e.entity_id)?.[1].before;
      const f: SyncFailure = {
        trackId: e.entity_id,
        name: before?.name ?? '',
        reason: String(p.reason ?? 'upload_error'),
        requestId: p.request_id as string | undefined,
        at: Date.now(),
      };
      for (const l of this.failureListeners) l(f);
    }
  }

  /** Applies a document from an accepted mutation response. */
  upsertTrack(t: Track): void {
    const cur = this.state.tracks.get(t.id);
    if (cur && t.revision < cur.revision) return;
    const tracks = new Map(this.state.tracks);
    tracks.set(t.id, t);
    this.commit({ tracks });
    this.emitTrack(t.id, { kind: 'upserted', before: cur, after: t });
  }

  removeTrack(id: string): void {
    const cur = this.state.tracks.get(id);
    if (!cur) return;
    const tracks = new Map(this.state.tracks);
    tracks.delete(id);
    this.commit({ tracks });
    this.emitTrack(id, { kind: 'deleted', before: cur });
  }

  upsertArtist(a: Artist): void {
    this.commit({ artists: mergeDocs(this.state.artists, [a], []) });
  }

  removeArtist(id: string): void {
    this.commit({ artists: mergeDocs(this.state.artists, [], [id]) });
  }

  upsertPlaylist(p: Playlist): void {
    this.commit({ playlists: mergeDocs(this.state.playlists, [p], []) });
  }

  removePlaylist(id: string): void {
    // Deleting a playlist also takes it out of its groups (R-ORG-5).
    const groups = new Map(this.state.groups);
    for (const [gid, g] of groups) {
      if (g.playlists.some((m) => m.playlist_id === id)) groups.set(gid, { ...g, playlists: g.playlists.filter((m) => m.playlist_id !== id) });
    }
    this.commit({ playlists: mergeDocs(this.state.playlists, [], [id]), groups });
  }

  upsertGroup(g: Group): void {
    this.commit({ groups: mergeDocs(this.state.groups, [g], []) });
  }

  removeGroup(id: string): void {
    this.commit({ groups: mergeDocs(this.state.groups, [], [id]) });
  }

  setPreferences(p: Preferences): void {
    if (p.revision >= this.state.preferences.revision) this.commit({ preferences: p });
  }

  setScopes(s: Scope[]): void {
    this.commit({ scopes: s });
  }

  setProgress(trackId: string, confirmed: number, declared: number): void {
    const progress = new Map(this.state.progress);
    progress.set(trackId, { confirmed, declared, at: Date.now() });
    this.commit({ progress });
  }
}

function mergeDocs<T extends { id: string; revision: number }>(cur: ReadonlyMap<string, T>, docs: T[], deleted: string[]): Map<string, T> {
  const out = new Map(cur);
  for (const d of docs) {
    const existing = out.get(d.id);
    if (!existing || d.revision >= existing.revision) out.set(d.id, d);
  }
  for (const id of deleted) out.delete(id);
  return out;
}
