import { describe, expect, it } from 'vitest';
import type { Track } from './api/types';
import { CatalogStore } from './catalog/store';
import { searchCatalog } from './search';

const at = '2026-10-01T00:00:00Z';
const track = (id: string, name: string, artist_id: string | null = null, pos = 'a0'): Track => ({
  id,
  name,
  artist_id,
  duration_ms: 1000,
  byte_size: 100,
  format: 'mp3',
  sha256: 'x',
  state: 'ready',
  current_content_id: `${id}-c`,
  origin_device_id: null,
  library_position: pos,
  metadata_suggestion: null,
  revision: 1,
  created_at: at,
  operation: null,
});

function catalog() {
  const s = new CatalogStore();
  s.loadSnapshot([
    {
      cursor: 1,
      tracks: [track('t1', 'Oh, Grey Warden', 'ar1', 'a0'), track('t2', 'Tavern Brawl', 'ar1', 'a1'), track('t3', 'The Last Octave', 'ar2', 'a2'), track('t4', 'Café Noir', null, 'a3')],
      artists: [
        { id: 'ar1', name: 'Raney Shockne', revision: 1, created_at: at },
        { id: 'ar2', name: 'Nymeria', revision: 1, created_at: at },
      ],
      playlists: [{ id: 'p1', name: 'Tavern Tracks', revision: 1, created_at: at, items: [{ track_id: 't1', position: 'a0' }, { track_id: 't2', position: 'a1' }] }],
      groups: [{ id: 'g1', name: 'Tavern nights', revision: 1, created_at: at, playlists: [{ playlist_id: 'p1', position: 'a0' }] }],
      preferences: { songs: true, playlists: true, groups: true, revision: 1 },
      scopes: [],
    },
  ]);
  return s.getState();
}

describe('searchCatalog', () => {
  it('finds every kind and ranks prefixes above matches anywhere', () => {
    const r = searchCatalog(catalog(), 'tav');
    expect(r.all.map((h) => h.name)).toEqual(['Tavern Tracks', 'Tavern nights', 'Tavern Brawl', 'The Last Octave']);
    expect(r.song.map((h) => h.id)).toEqual(['t2', 't3']);
    expect(r.playlist[0]).toMatchObject({ id: 'p1', count: 2, nameRanges: [[0, 3]] });
    expect(r.group[0]).toMatchObject({ id: 'g1', count: 1 });
    expect(r.artist).toEqual([]);
    expect(r.song[1]!.nameRanges).toEqual([[11, 14]]);
  });

  it('ignores case and accents, and maps ranges to the original text', () => {
    const r = searchCatalog(catalog(), 'CAFE');
    expect(r.song[0]).toMatchObject({ id: 't4', tier: 1, nameRanges: [[0, 4]] });
  });

  it('matches word starts', () => {
    const r = searchCatalog(catalog(), 'grey war');
    expect(r.song[0]).toMatchObject({ id: 't1', tier: 2, nameRanges: [[4, 8], [9, 12]] });
  });

  it('tolerates typos', () => {
    const r = searchCatalog(catalog(), 'raney shokne');
    expect(r.artist[0]).toMatchObject({ id: 'ar1', tier: 4 });
    expect(searchCatalog(catalog(), 'tavrn').playlist.map((h) => h.id)).toEqual(['p1']);
  });

  it("matches songs on their artist's name, below title matches", () => {
    const r = searchCatalog(catalog(), 'nymeria');
    expect(r.artist[0]).toMatchObject({ id: 'ar2', tier: 0, count: 1 });
    expect(r.song[0]).toMatchObject({ id: 't3', tier: 5, nameRanges: [], artistRanges: [[0, 7]] });
    expect(r.all.map((h) => h.kind)).toEqual(['artist', 'song']);
  });

  it('returns nothing for an empty query or no match', () => {
    expect(searchCatalog(catalog(), '   ').all).toEqual([]);
    expect(searchCatalog(catalog(), 'zzqx').all).toEqual([]);
  });
});
