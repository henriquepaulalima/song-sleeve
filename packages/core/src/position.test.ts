import { describe, expect, it } from 'vitest';
import { keyBetween } from './position';

describe('keyBetween', () => {
  it('matches the server implementation on known values', () => {
    const cases: [string | null, string | null, string][] = [
      [null, null, 'a0'],
      [null, 'a0', 'Zz'],
      [null, 'Zz', 'Zy'],
      ['a0', null, 'a1'],
      ['a1', null, 'a2'],
      ['a0', 'a1', 'a0V'],
      ['a1', 'a2', 'a1V'],
      ['a0V', 'a1', 'a0l'],
      ['Zz', 'a0', 'ZzV'],
      ['Zz', 'a1', 'a0'],
      [null, 'Y00', 'Xzzz'],
      ['bzz', null, 'c000'],
      ['a0', 'a0V', 'a0G'],
      ['a0', 'a0G', 'a08'],
      ['b125', 'b129', 'b127'],
      ['a0', 'a1V', 'a1'],
      ['Zz', 'a01', 'a0'],
      [null, 'a0V', 'a0'],
      [null, 'b999', 'b99'],
    ];
    for (const [a, b, want] of cases) expect(keyBetween(a, b)).toBe(want);
  });
  it('rejects invalid input', () => {
    expect(() => keyBetween('a0', 'a0')).toThrow();
    expect(() => keyBetween('a1', 'a0')).toThrow();
    expect(() => keyBetween('a00', null)).toThrow();
  });
});
