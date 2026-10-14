import { useId, useMemo, useRef, useState } from 'react';
import { MAX_ARTIST_NAME, cleanName, didYouMean, findSameArtist, searchArtists, type ArtistCandidate, type ArtistRef } from '@song-sleeve/core';
import { useCatalogSelect } from '../app/hooks';
import { useT } from '../app/i18n';
import { Icon } from './Icon';

export type ArtistChoice = { kind: 'none' } | { kind: 'existing'; id: string; name: string } | { kind: 'new'; name: string };

export function choiceToRef(c: ArtistChoice): ArtistRef | null {
  if (c.kind === 'existing') return { id: c.id };
  if (c.kind === 'new') return { name: c.name };
  return null;
}

export function choiceName(c: ArtistChoice): string | null {
  return c.kind === 'none' ? null : c.name;
}

/** The account's artists with song counts, for the matcher (R-ORG-2). */
export function useArtistCandidates(): (ArtistCandidate & { id: string })[] {
  return useCatalogSelect((s) => {
    const counts = new Map<string, number>();
    for (const t of s.tracks.values()) if (t.artist_id) counts.set(t.artist_id, (counts.get(t.artist_id) ?? 0) + 1);
    return [...s.artists.values()].map((a) => ({ id: a.id, name: a.name, songCount: counts.get(a.id) ?? 0 }));
  });
}

/** Resolves free text to a choice: an exact normalized match reuses the artist. */
export function textToChoice(text: string, artists: (ArtistCandidate & { id: string })[]): ArtistChoice {
  const name = cleanName(text);
  if (!name) return { kind: 'none' };
  const same = findSameArtist(name, artists);
  return same ? { kind: 'existing', id: same.id, name: same.name } : { kind: 'new', name };
}

/**
 * Smart artist search (R-ORG-2, D-10): searches as the user types, shows similar
 * names, and asks "Did you mean …?" before a new artist is created. The user
 * decides; nothing is merged or replaced automatically.
 */
export function ArtistField({
  value,
  onChange,
  label,
  compact,
  hint,
  autoFocus,
}: {
  value: ArtistChoice;
  onChange: (c: ArtistChoice) => void;
  label: string;
  compact?: boolean;
  /** A suggestion shown as a clickable chip (filename hint, embedded tag). */
  hint?: string | null;
  autoFocus?: boolean;
}) {
  const t = useT();
  const artists = useArtistCandidates();
  const [open, setOpen] = useState(false);
  const [text, setText] = useState(value.kind === 'none' ? '' : value.name);
  const [active, setActive] = useState(0);
  const listId = useId();
  const inputId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);

  const results = useMemo(() => (open ? searchArtists(text, artists) : []), [open, text, artists]);
  const typed = cleanName(text);
  const same = typed ? findSameArtist(typed, artists) : null;
  const suggestion = typed && !same ? didYouMean(typed, artists) : null;
  const options: { key: string; label: string; meta?: string; choice: ArtistChoice; isNew?: boolean }[] = [
    ...results.map((r) => ({ key: r.artist.id!, label: r.artist.name, meta: t('count.songs', { n: r.artist.songCount ?? 0 }), choice: { kind: 'existing', id: r.artist.id!, name: r.artist.name } as ArtistChoice })),
    ...(typed && !same && [...typed].length <= MAX_ARTIST_NAME ? [{ key: '__new', label: t('edit.newArtist', { name: typed }), choice: { kind: 'new', name: typed } as ArtistChoice, isNew: true }] : []),
  ];

  const pick = (c: ArtistChoice) => {
    onChange(c);
    setText(c.kind === 'none' ? '' : c.name);
    setOpen(false);
  };

  const commitText = () => {
    // Leaving the field keeps what was typed as a choice; an exact match reuses.
    pick(textToChoice(text, artists));
  };

  const note = value.kind === 'existing' ? t('edit.inLibrary') : value.kind === 'new' ? t('edit.newArtistNote') : null;

  if (!open && value.kind !== 'none') {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <button
          type="button"
          className={'input' + (compact ? ' compact' : '') + ' on-surface'}
          style={{ display: 'flex', alignItems: 'center', gap: 8, textAlign: 'left', cursor: 'pointer' }}
          onClick={() => {
            setOpen(true);
            setTimeout(() => inputRef.current?.focus(), 0);
          }}
          aria-label={`${label}: ${value.name}`}
        >
          <span className="ellipsis" style={{ padding: '3px 10px', borderRadius: 999, background: 'var(--sand)', color: 'var(--ink)', fontSize: 13, fontWeight: 600, maxWidth: '70%' }}>
            {value.name}
          </span>
          {!compact && <span style={{ fontSize: 12, color: 'var(--muted-2)' }}>{note}</span>}
          <Icon name="chevronDown" size={16} style={{ marginLeft: 'auto', color: 'var(--muted-2)' }} />
        </button>
        {compact && <span style={{ fontSize: 12, color: 'var(--muted-2)' }}>{note}</span>}
      </div>
    );
  }

  return (
    <div ref={wrapRef} style={{ position: 'relative', display: 'flex', flexDirection: 'column', gap: 6 }}>
      <label htmlFor={inputId} className="sr-only">
        {label}
      </label>
      <div className={'input' + (compact ? ' compact' : '') + ' on-surface'} style={{ display: 'flex', alignItems: 'center', gap: 10, ...(open ? { borderColor: 'var(--sand)', boxShadow: '0 0 0 3px rgba(229,220,197,.2)' } : null) }}>
        {!compact && <Icon name="search" size={18} style={{ color: 'var(--muted-2)' }} />}
        <input
          ref={inputRef}
          id={inputId}
          type="text"
          role="combobox"
          aria-expanded={open && options.length > 0}
          aria-controls={listId}
          aria-autocomplete="list"
          autoFocus={autoFocus}
          placeholder={t('edit.searchArtists')}
          value={text}
          maxLength={MAX_ARTIST_NAME * 2}
          onFocus={() => setOpen(true)}
          onChange={(e) => {
            setText(e.target.value);
            setOpen(true);
            setActive(0);
          }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault();
              setActive((a) => Math.min(a + 1, options.length - 1));
            } else if (e.key === 'ArrowUp') {
              e.preventDefault();
              setActive((a) => Math.max(a - 1, 0));
            } else if (e.key === 'Enter') {
              e.preventDefault();
              const o = options[active];
              if (o) pick(o.choice);
              else commitText();
            } else if (e.key === 'Escape') {
              e.stopPropagation();
              setOpen(false);
            }
          }}
          onBlur={(e) => {
            if (!wrapRef.current?.contains(e.relatedTarget as Node)) commitText();
          }}
          style={{ flex: 1, minWidth: 0, border: 0, background: 'transparent', color: 'var(--white)', font: 'inherit', outline: 'none' }}
        />
      </div>
      {hint && !open && value.kind === 'none' && (
        <button
          type="button"
          onClick={() => pick(textToChoice(hint, artists))}
          style={{ alignSelf: 'flex-start', display: 'inline-flex', alignItems: 'center', gap: 6, minHeight: 32, padding: '0 10px', borderRadius: 999, border: '1.5px dashed var(--sand)', background: 'transparent', color: 'var(--white)', font: '500 13px var(--font-ui)', cursor: 'pointer' }}
        >
          {t('add.useArtist', { name: hint })}
        </button>
      )}
      {open && (typed || hint) && (
        <div style={{ position: compact ? 'absolute' : 'static', top: compact ? '100%' : undefined, left: 0, right: 0, zIndex: 20, marginTop: 4 }}>
          {suggestion && (
            <div role="status" style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, padding: '10px 12px', borderRadius: 12, background: 'var(--sand)', color: 'var(--ink)', fontSize: 14, marginBottom: 4 }}>
              <span>{t('edit.didYouMean', { name: suggestion.name })}</span>
              <button
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => pick({ kind: 'existing', id: suggestion.id!, name: suggestion.name })}
                style={{ minHeight: 30, padding: '0 10px', borderRadius: 9, border: 0, background: 'var(--ink)', color: 'var(--sand)', font: '700 12px var(--font-ui)', cursor: 'pointer' }}
              >
                {t('edit.useIt')}
              </button>
            </div>
          )}
          {!typed && hint && (
            <button type="button" className="opt" onMouseDown={(e) => e.preventDefault()} onClick={() => pick(textToChoice(hint, artists))}>
              {t('add.useArtist', { name: hint })}
            </button>
          )}
          {options.length > 0 && (
            <ul id={listId} role="listbox" aria-label={label} style={{ listStyle: 'none', margin: 0, padding: 6, borderRadius: 16, background: 'var(--well)', boxShadow: 'var(--shadow-menu)', maxHeight: 280, overflowY: 'auto' }}>
              {options.map((o, i) => (
                <li key={o.key} role="option" aria-selected={i === active}>
                  <button type="button" className="opt" aria-selected={i === active} onMouseDown={(e) => e.preventDefault()} onClick={() => pick(o.choice)} style={{ minHeight: 48 }}>
                    <span
                      aria-hidden="true"
                      style={{
                        width: 32,
                        height: 32,
                        borderRadius: '50%',
                        flex: 'none',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        background: o.isNew ? 'var(--sand)' : 'var(--charcoal)',
                        color: o.isNew ? 'var(--ink)' : 'var(--sand)',
                        fontFamily: 'var(--font-display)',
                        fontStretch: '125%',
                        fontWeight: 800,
                        fontSize: 14,
                      }}
                    >
                      {o.isNew ? <Icon name="plus" size={16} strokeWidth={2.6} /> : o.label.charAt(0).toUpperCase()}
                    </span>
                    <span className="ellipsis" style={{ flex: 1 }}>
                      {o.label}
                    </span>
                    {o.meta && <span style={{ fontSize: 12, color: 'var(--muted-2)' }}>{o.meta}</span>}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
