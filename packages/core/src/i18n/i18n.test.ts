import { describe, expect, it } from 'vitest';
import { en } from './en';
import { ptBR } from './pt-BR';
import { formatBytes, formatClock, formatLength, translate } from './index';

const placeholders = (m: unknown): string[] => {
  const texts = typeof m === 'string' ? [m] : Object.values(m as Record<string, string>);
  return [...new Set(texts.flatMap((s) => [...s.matchAll(/\{(\w+)\}/g)].map((x) => x[1]!)))].sort();
};

describe('message catalog', () => {
  it('has every key in both languages, with the same placeholders (§8.8)', () => {
    expect(Object.keys(ptBR).sort()).toEqual(Object.keys(en).sort());
    for (const key of Object.keys(en) as (keyof typeof en)[]) {
      expect(placeholders(ptBR[key]), key).toEqual(placeholders(en[key]));
      expect(typeof ptBR[key], key).toBe(typeof en[key]);
    }
  });
  it('has no empty messages', () => {
    for (const [k, v] of Object.entries(ptBR)) {
      for (const s of typeof v === 'string' ? [v] : Object.values(v)) expect(s.trim(), k).not.toBe('');
    }
  });
});

describe('translate', () => {
  it('fills placeholders and picks plurals per locale', () => {
    expect(translate('en', 'count.songs', { n: 1 })).toBe('1 song');
    expect(translate('en', 'count.songs', { n: 1200 })).toBe('1,200 songs');
    expect(translate('pt-BR', 'count.songs', { n: 1200 })).toBe('1.200 músicas');
    expect(translate('pt-BR', 'status.syncing', { pct: 42 })).toBe('Sincronizando… 42%');
  });
});

describe('formatting', () => {
  it('formats clocks and lengths', () => {
    expect(formatClock(0)).toBe('0:00');
    expect(formatClock(232)).toBe('3:52');
    expect(formatClock(3725)).toBe('1:02:05');
    expect(formatClock(Number.NaN)).toBe('--:--');
    expect(formatLength('en', 52 * 60000)).toBe('52 min');
    expect(formatLength('en', 84 * 60000)).toBe('1 h 24 min');
    expect(formatLength('en', 120 * 60000)).toBe('2 h');
  });
  it('shows the limits as the plan names them', () => {
    expect(formatBytes('en', 134_217_728)).toBe('128 MB');
    expect(formatBytes('en', 26_843_545_600)).toBe('25 GB');
    expect(formatBytes('pt-BR', 9_332_326)).toBe('8,9 MB');
    expect(formatBytes('en', 500)).toBe('500 B');
  });
});
