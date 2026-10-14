/**
 * Translation layer (D-14, §4.4). Every UI string goes through it; user content
 * (song, artist, playlist and group names) is never translated.
 */
import { en } from './en';
import { ptBR } from './pt-BR';

export type { Locale } from '../api/types';
import type { Locale } from '../api/types';
export const LOCALES: { id: Locale; label: string }[] = [
  { id: 'en', label: 'English' },
  { id: 'pt-BR', label: 'Português (Brasil)' },
];

export type Plural = { one: string; other: string };
export type Message = string | Plural;
export type Messages = Record<keyof typeof en, Message>;
export type MessageKey = keyof typeof en;
export type Params = Record<string, string | number>;

const catalogs: Record<Locale, Messages> = { en, 'pt-BR': ptBR };

export function translate(locale: Locale, key: MessageKey, params: Params = {}): string {
  const msg = catalogs[locale][key] ?? en[key] ?? key;
  let s: string;
  if (typeof msg === 'string') s = msg;
  else {
    const n = Number(params.n ?? params.count ?? 0);
    s = new Intl.PluralRules(locale).select(n) === 'one' ? msg.one : msg.other;
  }
  return s.replace(/\{(\w+)\}/g, (_, k: string) => (params[k] !== undefined ? formatParam(locale, params[k]!) : `{${k}}`));
}

function formatParam(locale: Locale, v: string | number): string {
  return typeof v === 'number' ? new Intl.NumberFormat(locale).format(v) : v;
}

export function detectLocale(): Locale {
  const langs = typeof navigator !== 'undefined' ? navigator.languages ?? [navigator.language] : [];
  return langs.some((l) => l?.toLowerCase().startsWith('pt')) ? 'pt-BR' : 'en';
}

/** m:ss or h:mm:ss for times and durations. */
export function formatClock(totalSec: number): string {
  if (!Number.isFinite(totalSec) || totalSec < 0) return '--:--';
  const s = Math.floor(totalSec);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = String(s % 60).padStart(2, '0');
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${sec}` : `${m}:${sec}`;
}

/** "52 min" / "1 h 24 min" for list lengths. */
export function formatLength(locale: Locale, ms: number): string {
  const totalMin = Math.round(ms / 60000);
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  const nf = new Intl.NumberFormat(locale);
  if (h === 0) return `${nf.format(m)} min`;
  return m === 0 ? `${nf.format(h)} h` : `${nf.format(h)} h ${nf.format(m)} min`;
}

/**
 * Sizes as the UI shows them ("8.9 MB", "25 GB"). Binary units with the familiar
 * labels, so the 128 MiB song limit reads "128 MB" and the 25 GiB quota "25 GB".
 */
export function formatBytes(locale: Locale, bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = bytes;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  const digits = u === 0 ? 0 : v < 10 ? 1 : v < 100 ? 1 : 0;
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: digits, minimumFractionDigits: 0 }).format(v)} ${units[u]}`;
}

export function formatDate(locale: Locale, iso: string): string {
  return new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric' }).format(new Date(iso));
}

/** "Active now", "2 hours ago", "Oct 9". */
export function formatLastActive(locale: Locale, iso: string, now = Date.now()): string {
  const diff = now - new Date(iso).getTime();
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  if (diff < 10 * 60_000) return translate(locale, 'time.activeNow');
  if (diff < 3600_000) return translate(locale, 'time.lastActive', { when: rtf.format(-Math.round(diff / 60_000), 'minute') });
  if (diff < 86_400_000) return translate(locale, 'time.lastActive', { when: rtf.format(-Math.round(diff / 3600_000), 'hour') });
  return translate(locale, 'time.lastActive', { when: formatDate(locale, iso) });
}
