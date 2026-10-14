/** Song limits (R-LIM-1): 30 minutes AND 128 MiB, both inclusive. */
export const MAX_DURATION_SECONDS = 1800;
export const MAX_DURATION_MS = MAX_DURATION_SECONDS * 1000;
export const MAX_FILE_BYTES = 134_217_728;

export type AudioFormat = 'mp3' | 'aac' | 'flac' | 'wav';
export const FORMATS: readonly AudioFormat[] = ['mp3', 'aac', 'flac', 'wav'];

export type LimitError = 'song_too_long' | 'song_too_large' | 'too_long_and_large' | 'invalid_audio_metadata' | 'unsupported_format';

/** First layer of R-LIM-2: invalid rows can't be confirmed in the review list. */
export function checkLimits(facts: { durationMs?: number | null; byteSize?: number | null; format?: string | null }): LimitError | null {
  if (!facts.format || !FORMATS.includes(facts.format as AudioFormat)) return 'unsupported_format';
  const d = facts.durationMs;
  const s = facts.byteSize;
  if (d == null || !Number.isFinite(d) || d <= 0) return 'invalid_audio_metadata';
  if (s == null || !Number.isFinite(s) || s <= 0) return 'invalid_audio_metadata';
  const long = d > MAX_DURATION_MS;
  const large = s > MAX_FILE_BYTES;
  if (long && large) return 'too_long_and_large';
  if (long) return 'song_too_long';
  if (large) return 'song_too_large';
  return null;
}
