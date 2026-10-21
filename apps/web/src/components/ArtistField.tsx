import { useId, useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { createPortal } from 'react-dom';
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

  const typed = cleanName(text);
  // An empty field lists every artist by name; typing filters.
  const results = useMemo(
    () => (!open ? [] : typed ? searchArtists(text, artists) : [...artists].sort((a, b) => a.name.localeCompare(b.name)).map((artist) => ({ artist }))),
    [open, text, typed, artists],
  );
  const hintChoice = hint && !typed ? textToChoice(hint, artists) : null;
  const same = typed ? findSameArtist(typed, artists) : null;
  const suggestion = typed && !same ? didYouMean(typed, artists) : null;
  const options: { key: string; label: string; meta?: string; choice: ArtistChoice; isNew?: boolean }[] = [
    ...(hintChoice && hintChoice.kind !== 'none' ? [{ key: '__hint', label: t('add.useArtist', { name: hint! }), meta: t('add.fromFile'), choice: hintChoice, isNew: true }] : []),
    ...results.filter((r) => !(hintChoice?.kind === 'existing' && r.artist.id === hintChoice.id)).map((r) => ({ key: r.artist.id!, label: r.artist.name, meta: t('count.songs', { n: r.artist.songCount ?? 0 }), choice: { kind: 'existing', id: r.artist.id!, name: r.artist.name } as ArtistChoice })),
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

  const pop = usePopover(compact && open && options.length > 0, inputRef);

  const useHint = () => pick(textToChoice(hint!, artists));
  const useSuggestion = () => pick({ kind: 'existing', id: suggestion!.id!, name: suggestion!.name });

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
        {compact && <FieldSlot>{note}</FieldSlot>}
      </div>
    );
  }

  return (
    <div ref={wrapRef} style={{ position: 'relative', display: 'flex', flexDirection: 'column', gap: 6 }}>
      <label htmlFor={inputId} className="sr-only">
        {label}
      </label>
      <div style={{ position: 'relative' }}>
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
              const to = e.relatedTarget as Node | null;
              if (!wrapRef.current?.contains(to) && !pop.ref.current?.contains(to)) commitText();
            }}
            style={{ flex: 1, minWidth: 0, border: 0, background: 'transparent', color: 'var(--white)', font: 'inherit', outline: 'none' }}
          />
        </div>
        {open && options.length > 0 && portal(
          compact,
          <div ref={pop.ref} className={compact ? 'artist-pop' : undefined} style={compact ? pop.style : { marginTop: 4 }}>
              <ul id={listId} role="listbox" aria-label={label} onMouseDown={(e) => e.preventDefault()} style={{ listStyle: 'none', margin: 0, padding: 6, borderRadius: 16, background: 'var(--well)', boxShadow: 'var(--shadow-menu)', maxHeight: compact ? pop.maxHeight : 280, overflowY: 'auto' }}>
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
          </div>,
        )}
      </div>
      {compact ? (
        // One line that's always there, so opening the field doesn't change the row's height.
        <FieldSlot>
          {open && suggestion ? (
            <span role="status">
              {t('edit.didYouMean', { name: suggestion.name })}{' '}
              <button type="button" className="hint-link" onMouseDown={(e) => e.preventDefault()} onClick={useSuggestion}>
                {t('edit.useIt')}
              </button>
            </span>
          ) : hint && value.kind === 'none' && !typed && !open ? (
            <button type="button" className="hint-link" onMouseDown={(e) => e.preventDefault()} onClick={useHint}>
              {t('add.useArtist', { name: hint })}
            </button>
          ) : (
            note
          )}
        </FieldSlot>
      ) : (
        <>
          {hint && value.kind === 'none' && !typed && !open && (
            // The file's tag as a hint under the field, like form hint text.
            <button type="button" className="hint-link" onMouseDown={(e) => e.preventDefault()} onClick={useHint}>
              {t('add.useArtist', { name: hint })}
            </button>
          )}
          {open && suggestion && (
            <p role="status" className="field-hint">
              {t('edit.didYouMean', { name: suggestion.name })}{' '}
              <button type="button" className="hint-link" onMouseDown={(e) => e.preventDefault()} onClick={useSuggestion}>
                {t('edit.useIt')}
              </button>
            </p>
          )}
        </>
      )}
    </div>
  );
}

/** In a dialog's scrolling list the results float above the dialog, in a portal. */
function portal(floating: boolean | undefined, node: React.ReactNode) {
  return floating ? createPortal(node, document.body) : node;
}

/**
 * Places the floating results under the input, or above it when there's no room
 * below, and follows the input while anything scrolls.
 */
function usePopover(active: boolean | undefined, anchor: React.RefObject<HTMLElement | null>) {
  const ref = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState<{ style: CSSProperties; maxHeight: number }>({ style: { top: -9999, left: -9999 }, maxHeight: 280 });
  useLayoutEffect(() => {
    if (!active) return;
    const update = () => {
      const a = anchor.current?.closest('.input')?.getBoundingClientRect();
      if (!a) return;
      const below = window.innerHeight - a.bottom - 12;
      const above = a.top - 12;
      const up = below < 200 && above > below;
      const maxHeight = Math.min(280, Math.max(120, up ? above : below));
      const style: CSSProperties = up ? { left: a.left, width: a.width, bottom: window.innerHeight - a.top + 4 } : { left: a.left, width: a.width, top: a.bottom + 4 };
      setPlace({ style, maxHeight });
    };
    update();
    window.addEventListener('scroll', update, true);
    window.addEventListener('resize', update);
    return () => {
      window.removeEventListener('scroll', update, true);
      window.removeEventListener('resize', update);
    };
  }, [active, anchor]);
  return { ref, ...place };
}

/** The one-line note under a compact field: fixed height, cut with an ellipsis. */
function FieldSlot({ children }: { children: React.ReactNode }) {
  return <span className="field-slot">{children}</span>;
}
