import { describe, expect, it } from 'vitest';
import { checkPassword } from './password';
import { checkLimits, MAX_DURATION_MS, MAX_FILE_BYTES } from './limits';
import { artistKey, cleanName, nameProblem } from './fields';

describe('checkPassword (R-AUTH-9)', () => {
  it('needs 10–128 characters of any kind', () => {
    expect(checkPassword('123456789').length).toBe(false);
    expect(checkPassword('tide water').valid).toBe(true);
    expect(checkPassword('a'.repeat(128)).valid).toBe(true);
    expect(checkPassword('a'.repeat(129)).length).toBe(false);
    // Counts characters, not UTF-16 units.
    expect(checkPassword('🎵'.repeat(10)).length).toBe(true);
  });
  it('refuses common passwords case-insensitively', () => {
    expect(checkPassword('QWERTYUIOP').notCommon).toBe(false);
    expect(checkPassword('').notCommon).toBe(false);
  });
});

describe('checkLimits (R-LIM-1)', () => {
  it('accepts exactly 30 minutes and 128 MiB', () => {
    expect(checkLimits({ durationMs: MAX_DURATION_MS, byteSize: MAX_FILE_BYTES, format: 'flac' })).toBeNull();
  });
  it('flags each way of failing', () => {
    expect(checkLimits({ durationMs: MAX_DURATION_MS + 1, byteSize: 1, format: 'mp3' })).toBe('song_too_long');
    expect(checkLimits({ durationMs: 1, byteSize: MAX_FILE_BYTES + 1, format: 'mp3' })).toBe('song_too_large');
    expect(checkLimits({ durationMs: MAX_DURATION_MS + 1, byteSize: MAX_FILE_BYTES + 1, format: 'mp3' })).toBe('too_long_and_large');
    expect(checkLimits({ durationMs: null, byteSize: 1, format: 'mp3' })).toBe('invalid_audio_metadata');
    expect(checkLimits({ durationMs: Number.POSITIVE_INFINITY, byteSize: 1, format: 'mp3' })).toBe('invalid_audio_metadata');
    expect(checkLimits({ durationMs: 1, byteSize: 1, format: 'ogg' })).toBe('unsupported_format');
  });
});

describe('field limits (F-20)', () => {
  it('cleans names like the server', () => {
    expect(cleanName('  Har\u0000bor ')).toBe('Harbor');
    expect(nameProblem('   ', 10)).toBe('empty');
    expect(nameProblem('é'.repeat(101), 100)).toBe('too_long');
    expect(nameProblem('é'.repeat(100), 100)).toBeNull();
    expect(artistKey('  The   LOW tides')).toBe('the low tides');
  });
});
