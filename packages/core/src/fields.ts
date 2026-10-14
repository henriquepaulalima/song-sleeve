/** Field limits (F-20); the same limits apply to local songs. */
export const MAX_SONG_NAME = 200;
export const MAX_ARTIST_NAME = 200;
export const MAX_PLAYLIST_NAME = 100;
export const MAX_GROUP_NAME = 100;

// eslint-disable-next-line no-control-regex
const CONTROL = /[\u0000-\u001f\u007f-\u009f]/g;

/** Trims, NFC-normalizes and strips control characters, like the server. */
export function cleanName(s: string): string {
  return s.normalize('NFC').replace(CONTROL, '').trim();
}

export function charCount(s: string): number {
  return [...s].length;
}

export type NameProblem = 'empty' | 'too_long' | null;

export function nameProblem(s: string, max: number): NameProblem {
  const c = cleanName(s);
  if (!c) return 'empty';
  if (charCount(c) > max) return 'too_long';
  return null;
}

/** The artist identity key: "the same normalized name" (R-ORG-2), case and spacing only. */
export function artistKey(name: string): string {
  return cleanName(name).normalize('NFKC').toLowerCase().split(/\s+/).filter(Boolean).join(' ');
}
