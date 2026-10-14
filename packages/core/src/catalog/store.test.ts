import { describe, expect, it, vi } from 'vitest';
import type { Changes, Playlist, Track } from '../api/types';
import { CatalogStore } from './store';
import { libraryOrder, playlistTracks, songStatus, isPlayable, matchesQuery, newestFirst, groupPlaylists } from './selectors';

const track = (id: string, over: Partial<Track> = {}): Track => ({
  id,
  name: id,
  artist_id: null,
  duration_ms: 1000,
  byte_size: 100,
  format: 'mp3',
  sha256: 'x',
  state: 'ready',
  current_content_id: `${id}-c`,
  origin_device_id: null,
  library_position: 'a0',
  metadata_suggestion: null,
  revision: 1,
  created_at: '2026-10-01T00:00:00Z',
  operation: null,
  ...over,
});
const empty: Changes = { cursor: 0, has_more: false, events: [], account_changed: false, deleted: {} };

describe('CatalogStore', () => {
  it('loads a paged snapshot', () => {
    const s = new CatalogStore();
    s.loadSnapshot([
      { cursor: 7, tracks: [track('a')], artists: [], playlists: [], groups: [], preferences: { songs: true, playlists: false, groups: true, revision: 2 }, scopes: [] },
      { tracks: [track('b')] },
    ]);
    const st = s.getState();
    expect(st.loaded).toBe(true);
    expect(st.cursor).toBe(7);
    expect([...st.tracks.keys()]).toEqual(['a', 'b']);
    expect(st.preferences.playlists).toBe(false);
  });

  it('ignores older revisions and applies equal or newer ones (R-EVT-3)', () => {
    const s = new CatalogStore();
    s.loadSnapshot([{ cursor: 1, tracks: [track('a', { revision: 3, name: 'v3' })] }]);
    s.applyChanges({ ...empty, cursor: 2, tracks: [track('a', { revision: 2, name: 'old' })] }, null);
    expect(s.getState().tracks.get('a')!.name).toBe('v3');
    s.applyChanges({ ...empty, cursor: 3, tracks: [track('a', { revision: 3, name: 'same rev, newer op' })] }, null);
    expect(s.getState().tracks.get('a')!.name).toBe('same rev, newer op');
    s.upsertTrack(track('a', { revision: 1, name: 'stale response' }));
    expect(s.getState().tracks.get('a')!.name).toBe('same rev, newer op');
    expect(s.getState().cursor).toBe(3);
  });

  it('never moves the cursor backwards', () => {
    const s = new CatalogStore();
    s.loadSnapshot([{ cursor: 10, tracks: [] }]);
    s.applyChanges({ ...empty, cursor: 4 }, null);
    expect(s.getState().cursor).toBe(10);
  });

  it('applies deletions and notifies track listeners', () => {
    const s = new CatalogStore();
    s.loadSnapshot([{ cursor: 1, tracks: [track('a'), track('b')] }]);
    const seen: string[] = [];
    s.onTrackChange((id, c) => seen.push(`${id}:${c.kind}`));
    s.applyChanges({ ...empty, cursor: 2, tracks: [track('b', { revision: 2 })], deleted: { track: ['a'] } }, null);
    expect([...s.getState().tracks.keys()]).toEqual(['b']);
    expect(seen).toEqual(['b:upserted', 'a:deleted']);
  });

  it('reports sync failures only for this device (R-ID-4)', () => {
    const s = new CatalogStore();
    s.loadSnapshot([{ cursor: 1, tracks: [track('a', { name: 'Mine', state: 'pending_upload' }), track('b', { state: 'pending_upload' })] }]);
    const failures = vi.fn();
    s.onSyncFailure(failures);
    s.applyChanges(
      {
        ...empty,
        cursor: 3,
        deleted: { track: ['a', 'b'] },
        events: [
          { cursor: 2, entity_type: 'track', entity_id: 'a', event_type: 'track_sync_failed', revision: 0, payload: { reason: 'stalled', origin_device_id: 'me' } },
          { cursor: 3, entity_type: 'track', entity_id: 'b', event_type: 'track_sync_failed', revision: 0, payload: { reason: 'stalled', origin_device_id: 'other' } },
        ],
      },
      'me',
    );
    expect(failures).toHaveBeenCalledTimes(1);
    expect(failures.mock.calls[0]![0]).toMatchObject({ trackId: 'a', name: 'Mine', reason: 'stalled' });
  });

  it('drops progress hints once an authoritative document arrives', () => {
    const s = new CatalogStore();
    s.loadSnapshot([{ cursor: 1, tracks: [track('a')] }]);
    s.setProgress('a', 50, 100);
    expect(s.getState().progress.get('a')).toMatchObject({ confirmed: 50 });
    s.applyChanges({ ...empty, cursor: 2, tracks: [track('a', { revision: 2 })] }, null);
    expect(s.getState().progress.has('a')).toBe(false);
  });

  it('removing a playlist takes it out of its groups (R-ORG-5)', () => {
    const s = new CatalogStore();
    const p: Playlist = { id: 'p', name: 'P', revision: 1, created_at: '', items: [] };
    s.loadSnapshot([{ cursor: 1, tracks: [], playlists: [p], groups: [{ id: 'g', name: 'G', revision: 1, created_at: '', playlists: [{ playlist_id: 'p', position: 'a0' }] }] }]);
    s.removePlaylist('p');
    expect(s.getState().groups.get('g')!.playlists).toEqual([]);
    expect(groupPlaylists(s.getState(), 'g')).toEqual([]);
  });

  it('notifies subscribers with a new state object', () => {
    const s = new CatalogStore();
    const fn = vi.fn();
    const off = s.subscribe(fn);
    const before = s.getState();
    s.setScopes([]);
    expect(fn).toHaveBeenCalledOnce();
    expect(s.getState()).not.toBe(before);
    off();
    s.setScopes([]);
    expect(fn).toHaveBeenCalledOnce();
  });
});

describe('selectors', () => {
  const st = () => {
    const s = new CatalogStore();
    s.loadSnapshot([
      {
        cursor: 1,
        tracks: [track('c', { library_position: 'a2', created_at: '2026-10-03T00:00:00Z' }), track('a', { library_position: 'a0' }), track('b', { library_position: 'a1', created_at: '2026-10-02T00:00:00Z' })],
        playlists: [{ id: 'p', name: 'P', revision: 1, created_at: '', items: [{ track_id: 'c', position: 'a0' }, { track_id: 'gone', position: 'a1' }, { track_id: 'a', position: 'Zz' }] }],
      },
    ]);
    return s.getState();
  };
  it('orders the library and playlists by position key', () => {
    expect(libraryOrder(st()).map((t) => t.id)).toEqual(['a', 'b', 'c']);
    // Byte order (COLLATE "C"): "Zz" < "a0"; missing tracks are skipped.
    expect(playlistTracks(st(), 'p').map((t) => t.id)).toEqual(['a', 'c']);
  });
  it('sorts newest first by creation time (D-36)', () => {
    expect(newestFirst(st().tracks.values()).map((t) => t.id)).toEqual(['c', 'b', 'a']);
  });
  it('derives the status label from the operation (§3)', () => {
    expect(songStatus(track('x'))).toEqual({ kind: 'synced' });
    const op = { id: 'o', state: 'active' as const, phase: 'uploading' as const, confirmed_bytes: 25, declared_bytes: 100, owner_device_id: null };
    expect(songStatus(track('x', { state: 'pending_upload', operation: { ...op, kind: 'initial_upload' } }))).toEqual({ kind: 'syncing', pct: 25 });
    expect(songStatus(track('x', { state: 'pending_upload', operation: { ...op, kind: 'initial_upload' } }), { confirmed: 60, declared: 100, at: 0 })).toEqual({ kind: 'syncing', pct: 60 });
    expect(songStatus(track('x', { operation: { ...op, kind: 'replace', phase: 'verifying' } }))).toEqual({ kind: 'updating', pct: 100 });
    expect(songStatus(track('x', { operation: { ...op, kind: 'replace', state: 'waiting' } }))).toEqual({ kind: 'waiting' });
    expect(songStatus(track('x', { metadata_suggestion: { name: 'n' } }))).toEqual({ kind: 'attention' });
  });
  it('only ready tracks with content are playable (R-PLAY-3)', () => {
    expect(isPlayable(track('x'))).toBe(true);
    expect(isPlayable(track('x', { state: 'pending_upload' }))).toBe(false);
    expect(isPlayable(track('x', { current_content_id: null }))).toBe(false);
    expect(isPlayable(undefined)).toBe(false);
  });
  it('searches ignoring case and accents', () => {
    expect(matchesQuery('beyonce', 'Halo', 'Beyoncé')).toBe(true);
    expect(matchesQuery('  ', 'anything')).toBe(true);
    expect(matchesQuery('zep', 'Halo', null)).toBe(false);
  });
});
