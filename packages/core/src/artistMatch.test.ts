import { describe, expect, it } from 'vitest';
import { artistHintFromName, didYouMean, findSameArtist, searchArtists } from './artistMatch';

const artists = [
  { id: '1', name: 'The Beatles' },
  { id: '2', name: 'AC/DC' },
  { id: '3', name: 'Beyoncé' },
  { id: '4', name: 'Metallica' },
  { id: '5', name: 'Led Zeppelin' },
  { id: '6', name: "Guns N' Roses" },
  { id: '7', name: 'Marina Okafor' },
  { id: '8', name: 'Marigold Trio' },
  { id: '9', name: 'The Low Tides' },
];

const names = (q: string) => searchArtists(q, artists).map((m) => m.artist.name);

describe('searchArtists (R-ORG-2 exit tests)', () => {
  it('offers The Beatles for Beatles', () => expect(names('Beatles')[0]).toBe('The Beatles'));
  it('offers AC/DC for ACDC', () => expect(names('ACDC')[0]).toBe('AC/DC'));
  it('offers Beyoncé for Beyonce', () => expect(names('Beyonce')[0]).toBe('Beyoncé'));
  it('offers Metallica for Metalica', () => expect(names('Metalica')).toContain('Metallica'));
  it('matches word starts', () => expect(names('zep')).toContain('Led Zeppelin'));
  it('treats &, and, n\' alike', () => expect(names('Guns and Roses')[0]).toBe("Guns N' Roses"));
  it('ranks exact, then prefix, then similar', () => {
    const res = searchArtists('mari', artists);
    expect(res.map((r) => r.kind)).toEqual(['prefix', 'prefix']);
    expect(searchArtists('Marina Okafor', artists)[0]).toMatchObject({ kind: 'exact' });
  });
  it('caps results at 8', () => {
    const many = Array.from({ length: 20 }, (_, i) => ({ name: `Band ${i}` }));
    expect(searchArtists('band', many)).toHaveLength(8);
  });
  it('returns nothing for empty input', () => expect(searchArtists('  ', artists)).toEqual([]));
});

describe('didYouMean', () => {
  it('suggests a similar existing artist', () => {
    expect(didYouMean('Marina Okafr', artists)?.name).toBe('Marina Okafor');
    expect(didYouMean('Low Tides', artists)?.name).toBe('The Low Tides');
    expect(didYouMean('Beatles', artists)?.name).toBe('The Beatles');
  });
  it('stays quiet for exact names and unrelated names', () => {
    expect(didYouMean('marina okafor', artists)).toBeNull();
    expect(didYouMean('Paulo Verde', artists)).toBeNull();
  });
});

describe('findSameArtist', () => {
  it('folds case and spacing only', () => {
    expect(findSameArtist('  the   beatles ', artists)?.id).toBe('1');
    expect(findSameArtist('Beatles', artists)).toBeNull();
  });
});

describe('artistHintFromName', () => {
  it('splits once at the first hyphen', () => {
    expect(artistHintFromName('Harbor Lights - Marina Okafor')).toBe('Marina Okafor');
    expect(artistHintFromName('A - B - C')).toBe('B - C');
    expect(artistHintFromName('No hyphen')).toBeNull();
  });
});
