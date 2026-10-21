/**
 * Search anywhere (designs/proposals/Round3 #search): one matcher over songs,
 * artists, playlists and groups. Case and accents don't matter. Ranking: the whole
 * name, then names starting with the text, then word starts, then the text
 * anywhere, then close spellings. Songs also match on their artist's name, below
 * any match on a title. Hits carry the matched ranges for highlighting.
 */
import { editDistance, typoBudget } from './artistMatch';
import type { CatalogState } from './catalog/store';
import { groupPlaylists, libraryOrder, playlistTracks } from './catalog/selectors';

export type SearchKind = 'song' | 'artist' | 'playlist' | 'group';

/** [start, end) in the original string. */
export type Range = [number, number];

export interface SearchHit {
  kind: SearchKind;
  id: string;
  name: string;
  /** 0 = whole name … 4 = close spelling; +5 when a song matched only on its artist. */
  tier: number;
  nameRanges: Range[];
  /** Songs: the artist's name, and where it matched. Others: unset. */
  artist?: string;
  artistRanges?: Range[];
  /** Song, playlist or artist count shown under the name. */
  count?: number;
}

export interface SearchResults {
  all: SearchHit[];
  song: SearchHit[];
  artist: SearchHit[];
  playlist: SearchHit[];
  group: SearchHit[];
}

const KIND_ORDER: Record<SearchKind, number> = { artist: 0, playlist: 1, group: 2, song: 3 };

/** A folded copy of the text and, for each folded character, its index in the original. */
function foldWithMap(text: string): { folded: string; map: number[] } {
  let folded = '';
  const map: number[] = [];
  let i = 0;
  for (const ch of text) {
    const f = ch.normalize('NFKD').replace(/\p{M}+/gu, '').toLowerCase();
    for (let k = 0; k < f.length; k++) map.push(i);
    folded += f;
    i += ch.length;
  }
  map.push(i);
  return { folded, map };
}

function fold(text: string): string {
  return text.normalize('NFKD').replace(/\p{M}+/gu, '').toLowerCase().replace(/\s+/g, ' ').trim();
}

const wordRe = /[\p{L}\p{N}]+/gu;

interface Match {
  tier: number;
  ranges: Range[];
}

/** Matches one field; null when it doesn't match at all. */
function matchField(query: string, qWords: string[], text: string): Match | null {
  if (!text) return null;
  const { folded, map } = foldWithMap(text);
  const toRange = (start: number, end: number): Range => [map[start]!, map[end]!];
  const norm = folded.replace(/\s+/g, ' ').trim();
  if (norm === query) return { tier: 0, ranges: [[0, text.length]] };
  if (folded.trimStart().startsWith(query)) {
    const at = folded.indexOf(query);
    return { tier: 1, ranges: [toRange(at, at + query.length)] };
  }
  const words = [...folded.matchAll(wordRe)].map((m) => ({ w: m[0], at: m.index! }));
  // Word starts: every query word starts some word of the text ("grey war" → Oh, Grey Warden).
  const starts = qWords.map((q) => words.find((x) => x.w.startsWith(q)));
  if (qWords.length > 0 && starts.every(Boolean)) {
    return { tier: 2, ranges: starts.map((x, i) => toRange(x!.at, x!.at + qWords[i]!.length)) };
  }
  const at = folded.indexOf(query);
  if (at >= 0) return { tier: 3, ranges: [toRange(at, at + query.length)] };
  // Close spellings, word by word: each query word is within a typo budget of a word
  // of the text, or of its start ("raney shokne" → Raney Shockne, "tavrn" → Tavern).
  const close = qWords.map((q) => {
    const budget = typoBudget(q.length);
    if (budget === 0) return words.find((x) => x.w.startsWith(q));
    return words.find((x) => editDistance(q, x.w, budget) <= budget || (x.w.length > q.length && editDistance(q, x.w.slice(0, q.length), budget) <= budget));
  });
  if (qWords.length > 0 && close.every(Boolean) && qWords.some((q) => typoBudget(q.length) > 0)) {
    return { tier: 4, ranges: close.map((x) => toRange(x!.at, x!.at + x!.w.length)) };
  }
  return null;
}

function byRank(a: SearchHit, b: SearchHit): number {
  return a.tier - b.tier || KIND_ORDER[a.kind] - KIND_ORDER[b.kind] || a.name.localeCompare(b.name);
}

/**
 * Searches the catalog. Each kind keeps up to `perKind` hits (200 by default);
 * `all` merges them by rank, up to `allLimit` (50).
 */
export function searchCatalog(s: CatalogState, rawQuery: string, opts: { perKind?: number; allLimit?: number } = {}): SearchResults {
  const empty: SearchResults = { all: [], song: [], artist: [], playlist: [], group: [] };
  const query = fold(rawQuery);
  if (!query) return empty;
  const qWords = [...query.matchAll(wordRe)].map((m) => m[0]);
  const perKind = opts.perKind ?? 200;
  const out = empty;

  const songCounts = new Map<string, number>();
  for (const t of s.tracks.values()) if (t.artist_id) songCounts.set(t.artist_id, (songCounts.get(t.artist_id) ?? 0) + 1);

  for (const t of libraryOrder(s)) {
    const artist = t.artist_id ? s.artists.get(t.artist_id)?.name : undefined;
    const m = matchField(query, qWords, t.name);
    const a = artist ? matchField(query, qWords, artist) : null;
    if (!m && !a) continue;
    out.song.push({ kind: 'song', id: t.id, name: t.name, tier: m ? m.tier : a!.tier + 5, nameRanges: m?.ranges ?? [], artist, artistRanges: m ? [] : a!.ranges });
  }
  for (const ar of s.artists.values()) {
    const m = matchField(query, qWords, ar.name);
    if (m) out.artist.push({ kind: 'artist', id: ar.id, name: ar.name, tier: m.tier, nameRanges: m.ranges, count: songCounts.get(ar.id) ?? 0 });
  }
  for (const p of s.playlists.values()) {
    const m = matchField(query, qWords, p.name);
    if (m) out.playlist.push({ kind: 'playlist', id: p.id, name: p.name, tier: m.tier, nameRanges: m.ranges, count: playlistTracks(s, p.id).length });
  }
  for (const g of s.groups.values()) {
    const m = matchField(query, qWords, g.name);
    if (m) out.group.push({ kind: 'group', id: g.id, name: g.name, tier: m.tier, nameRanges: m.ranges, count: groupPlaylists(s, g.id).length });
  }
  for (const k of ['song', 'artist', 'playlist', 'group'] as const) out[k] = out[k].sort(byRank).slice(0, perKind);
  out.all = [...out.artist, ...out.playlist, ...out.group, ...out.song].sort(byRank).slice(0, opts.allLimit ?? 50);
  return out;
}
