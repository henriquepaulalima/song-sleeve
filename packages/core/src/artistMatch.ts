/**
 * Smart artist search (R-ORG-2, D-10). One matcher for every platform: it runs on the
 * artist list the client already has. Similarity ignores case, accents, punctuation
 * and spacing, a leading "The", and &/and/n'; it tolerates small typos and matches
 * word starts. Ranking: exact, then names starting with the text, then similar
 * names; at most 8 results. Nothing is ever merged automatically.
 */
import { artistKey } from './fields';

export interface ArtistCandidate {
  /** Server artist ID; absent for names used only by this device's local songs. */
  id?: string;
  name: string;
  songCount?: number;
  local?: boolean;
}

export type MatchKind = 'exact' | 'prefix' | 'similar';

export interface ArtistMatch<T extends ArtistCandidate = ArtistCandidate> {
  artist: T;
  kind: MatchKind;
}

/** Words after folding: lowercase, no accents, &/and/n' as "and", no punctuation, no leading "the". */
function words(name: string): string[] {
  let s = name.normalize('NFKD').replace(/\p{M}+/gu, '').toLowerCase();
  s = s.replace(/&/g, ' and ').replace(/\+/g, ' and ');
  let out = s
    .split(/\s+/)
    .map((w) => (/^'?n'?$/.test(w) ? 'and' : w))
    .map((w) => w.replace(/[^\p{L}\p{N}]+/gu, ''))
    .filter(Boolean);
  if (out.length > 1 && out[0] === 'the') out = out.slice(1);
  return out;
}

/** Spacing-insensitive comparison key. */
export function similarityKey(name: string): string {
  return words(name).join('');
}

/** Optimal string alignment distance, bounded for speed. */
function editDistance(a: string, b: string, max: number): number {
  if (Math.abs(a.length - b.length) > max) return max + 1;
  const prev2: number[] = new Array(b.length + 1).fill(0);
  let prev: number[] = Array.from({ length: b.length + 1 }, (_, j) => j);
  for (let i = 1; i <= a.length; i++) {
    const cur: number[] = [i];
    let rowMin = i;
    for (let j = 1; j <= b.length; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      let v = Math.min(prev[j]! + 1, cur[j - 1]! + 1, prev[j - 1]! + cost);
      if (i > 1 && j > 1 && a[i - 1] === b[j - 2] && a[i - 2] === b[j - 1]) {
        v = Math.min(v, prev2[j - 2]! + 1);
      }
      cur[j] = v;
      rowMin = Math.min(rowMin, v);
    }
    for (let j = 0; j <= b.length; j++) prev2[j] = prev[j]!;
    prev = cur;
    if (rowMin > max) return max + 1;
  }
  return prev[b.length]!;
}

function typoBudget(len: number): number {
  if (len < 4) return 0;
  if (len < 8) return 1;
  return 2;
}

function classify(query: string, qWords: string[], qKey: string, name: string): MatchKind | null {
  if (!qKey) return null;
  const nKey = similarityKey(name);
  if (!nKey) return null;
  if (artistKey(name) === artistKey(query) || nKey === qKey) return 'exact';
  if (nKey.startsWith(qKey) || artistKey(name).startsWith(artistKey(query))) return 'prefix';
  const nWords = words(name);
  // Word starts: every query word starts some word of the name ("zep" → Led Zeppelin).
  if (qWords.length > 0 && qWords.every((qw) => nWords.some((nw) => nw.startsWith(qw)))) return 'similar';
  const budget = typoBudget(Math.min(qKey.length, nKey.length));
  if (budget > 0 && editDistance(qKey, nKey, budget) <= budget) return 'similar';
  // A typo in a partially typed name: compare with the same-length start of the name.
  if (qKey.length >= 5 && nKey.length > qKey.length) {
    const b = typoBudget(qKey.length);
    if (editDistance(qKey, nKey.slice(0, qKey.length), b) <= b) return 'similar';
  }
  return null;
}

const ORDER: Record<MatchKind, number> = { exact: 0, prefix: 1, similar: 2 };

/** Searches as the user types. Results are ranked and capped at `limit` (8). */
export function searchArtists<T extends ArtistCandidate>(query: string, artists: readonly T[], limit = 8): ArtistMatch<T>[] {
  const qWords = words(query);
  const qKey = qWords.join('');
  if (!qKey) return [];
  const out: ArtistMatch<T>[] = [];
  for (const artist of artists) {
    const kind = classify(query, qWords, qKey, artist.name);
    if (kind) out.push({ artist, kind });
  }
  out.sort((a, b) => ORDER[a.kind] - ORDER[b.kind] || (b.artist.songCount ?? 0) - (a.artist.songCount ?? 0) || a.artist.name.localeCompare(b.artist.name));
  return out.slice(0, limit);
}

/** The artist with exactly the same normalized name, which saving would reuse. */
export function findSameArtist<T extends ArtistCandidate>(name: string, artists: readonly T[]): T | null {
  const key = artistKey(name);
  if (!key) return null;
  return artists.find((a) => artistKey(a.name) === key) ?? null;
}

/**
 * "Did you mean {artist}?" before a new artist is created: the best similar artist
 * when the typed name isn't an existing one. The user decides.
 */
export function didYouMean<T extends ArtistCandidate>(typed: string, artists: readonly T[]): T | null {
  if (!artistKey(typed) || findSameArtist(typed, artists)) return null;
  const qWords = words(typed);
  const qKey = qWords.join('');
  let best: { artist: T; score: number } | null = null;
  for (const artist of artists) {
    const nKey = similarityKey(artist.name);
    let score: number | null = null;
    if (nKey === qKey) score = 0;
    else {
      const budget = typoBudget(Math.min(qKey.length, nKey.length));
      const d = budget > 0 ? editDistance(qKey, nKey, budget) : budget + 1;
      if (d <= budget) score = d;
      else if (qKey.length >= 3 && nKey.startsWith(qKey) && qKey.length / nKey.length >= 0.6) score = 3;
    }
    if (score !== null && (!best || score < best.score)) best = { artist, score };
  }
  return best?.artist ?? null;
}

/**
 * The filename hint (§7.1 step 2): if a name contains a hyphen, split it once and
 * offer the trimmed second part as an artist suggestion.
 */
export function artistHintFromName(name: string): string | null {
  const i = name.indexOf('-');
  if (i < 0) return null;
  const hint = name.slice(i + 1).trim();
  return hint || null;
}
