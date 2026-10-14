import type { Artist, Group, Playlist, Track } from '../api/types';
import type { CatalogState, SyncProgress } from './store';

const byPosition = (a: { position: string; id: string }, b: { position: string; id: string }) =>
  a.position !== b.position ? (a.position < b.position ? -1 : 1) : a.id < b.id ? -1 : a.id > b.id ? 1 : 0;

/** Every song in library order (R-ORG-6). */
export function libraryOrder(s: CatalogState): Track[] {
  return [...s.tracks.values()].sort((a, b) => byPosition({ position: a.library_position, id: a.id }, { position: b.library_position, id: b.id }));
}

export function playlistTracks(s: CatalogState, playlistId: string): Track[] {
  const p = s.playlists.get(playlistId);
  if (!p) return [];
  return [...p.items]
    .sort((a, b) => byPosition({ position: a.position, id: a.track_id }, { position: b.position, id: b.track_id }))
    .map((i) => s.tracks.get(i.track_id))
    .filter((t): t is Track => !!t);
}

export function groupPlaylists(s: CatalogState, groupId: string): Playlist[] {
  const g = s.groups.get(groupId);
  if (!g) return [];
  return [...g.playlists]
    .sort((a, b) => byPosition({ position: a.position, id: a.playlist_id }, { position: b.position, id: b.playlist_id }))
    .map((m) => s.playlists.get(m.playlist_id))
    .filter((p): p is Playlist => !!p);
}

export function groupsOfPlaylist(s: CatalogState, playlistId: string): Group[] {
  return [...s.groups.values()].filter((g) => g.playlists.some((m) => m.playlist_id === playlistId));
}

export function playlistsOfTrack(s: CatalogState, trackId: string): Playlist[] {
  return [...s.playlists.values()].filter((p) => p.items.some((i) => i.track_id === trackId));
}

export function artistTracks(s: CatalogState, artistId: string): Track[] {
  return libraryOrder(s).filter((t) => t.artist_id === artistId);
}

/** Newest first by creation time (Home, D-36). */
export function newestFirst<T extends { created_at: string }>(items: Iterable<T>): T[] {
  return [...items].sort((a, b) => (a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : 0));
}

export function artistsByName(s: CatalogState): Artist[] {
  return [...s.artists.values()].sort((a, b) => a.name.localeCompare(b.name));
}

export function songCountByArtist(s: CatalogState): Map<string, number> {
  const out = new Map<string, number>();
  for (const t of s.tracks.values()) if (t.artist_id) out.set(t.artist_id, (out.get(t.artist_id) ?? 0) + 1);
  return out;
}

export function totalDurationMs(tracks: Track[]): number {
  return tracks.reduce((n, t) => n + t.duration_ms, 0);
}

/** The status shown on a synced song (§3). */
export type SongStatus =
  | { kind: 'synced' }
  | { kind: 'syncing'; pct: number }
  | { kind: 'verifying' }
  | { kind: 'updating'; pct: number }
  | { kind: 'waiting' }
  | { kind: 'attention' };

export function songStatus(t: Track, progress?: SyncProgress): SongStatus {
  const op = t.operation;
  const pct = (confirmed: number, declared: number) => (declared > 0 ? Math.min(100, Math.floor((confirmed / declared) * 100)) : 0);
  if (op) {
    if (op.state === 'waiting') return { kind: 'waiting' };
    const confirmed = Math.max(op.confirmed_bytes, progress?.confirmed ?? 0);
    const declared = op.declared_bytes || progress?.declared || 0;
    if (op.kind === 'initial_upload') return op.phase === 'verifying' ? { kind: 'syncing', pct: 100 } : { kind: 'syncing', pct: pct(confirmed, declared) };
    if (op.kind === 'replace') return { kind: 'updating', pct: op.phase === 'verifying' ? 100 : pct(confirmed, declared) };
    if (op.kind === 'delete') return { kind: 'waiting' };
  }
  if (t.metadata_suggestion) return { kind: 'attention' };
  return { kind: 'synced' };
}

export function isPlayable(t: Track | undefined): boolean {
  return !!t && t.state === 'ready' && !!t.current_content_id;
}

/** Case- and accent-insensitive contains, for list search. */
export function matchesQuery(query: string, ...fields: (string | null | undefined)[]): boolean {
  const fold = (s: string) => s.normalize('NFKD').replace(/\p{M}+/gu, '').toLowerCase();
  const q = fold(query.trim());
  if (!q) return true;
  return fields.some((f) => f && fold(f).includes(q));
}
