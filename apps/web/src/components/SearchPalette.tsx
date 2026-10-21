import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router';
import { libraryOrder, searchCatalog, type MessageKey, type Range, type SearchHit, type SearchKind } from '@song-sleeve/core';
import { useCatalog, useSignal } from '../app/hooks';
import { useT } from '../app/i18n';
import { searchOpen } from '../app/search';
import { catalog, player } from '../app/services';
import { Icon, type IconName } from './Icon';

type Tab = 'all' | SearchKind;
const TABS: Tab[] = ['all', 'song', 'artist', 'playlist', 'group'];
const TAB_LABEL: Record<Tab, MessageKey> = { all: 'search.all', song: 'search.songs', artist: 'search.artists', playlist: 'search.playlists', group: 'search.groups' };
const KIND_LABEL: Record<SearchKind, MessageKey> = { song: 'kind.song', artist: 'kind.artist', playlist: 'kind.playlist', group: 'kind.group' };
const KIND_ICON: Record<SearchKind, IconName> = { song: 'note', artist: 'artist', playlist: 'playlist', group: 'group' };

/** Mounted once for signed-in screens; renders the box while it's open. */
export function SearchHost() {
  const open = useSignal(searchOpen);
  return open ? <SearchPalette onClose={() => searchOpen.set(false)} /> : null;
}

/** The text with its matched ranges in sand. */
function Highlight({ text, ranges }: { text: string; ranges?: Range[] }) {
  if (!ranges?.length) return <>{text}</>;
  const out: ReactNode[] = [];
  let at = 0;
  for (const [a, b] of [...ranges].sort((x, y) => x[0] - y[0])) {
    if (a < at) continue;
    if (a > at) out.push(text.slice(at, a));
    out.push(<mark key={a}>{text.slice(a, b)}</mark>);
    at = b;
  }
  out.push(text.slice(at));
  return <>{out}</>;
}

/**
 * Search anywhere (designs/proposals/Round3 #search). One list of matches, best
 * first: ↑↓ move through it, ←→ switch the kind tabs (like the Claude CLI), Enter
 * opens. A song plays at once and Songs scrolls to it.
 */
function SearchPalette({ onClose }: { onClose: () => void }) {
  const t = useT();
  const navigate = useNavigate();
  const s = useCatalog();
  const [q, setQ] = useState('');
  const [tab, setTab] = useState<Tab>('all');
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const listId = useId();
  const tabsId = useId();

  const results = useMemo(() => searchCatalog(s, q), [s, q]);
  const counts: Record<Tab, number> = { all: results.song.length + results.artist.length + results.playlist.length + results.group.length, song: results.song.length, artist: results.artist.length, playlist: results.playlist.length, group: results.group.length };
  const hits = results[tab];
  const typed = q.trim().length > 0;

  // A new query starts over on the first row of All.
  useEffect(() => {
    setTab('all');
    setActive(0);
  }, [q]);

  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    inputRef.current?.focus();
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = overflow;
      prev?.focus?.();
    };
  }, []);

  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [active, tab]);

  const open = (hit: SearchHit) => {
    onClose();
    if (hit.kind === 'song') {
      // Plays from the library order, like starting it from Songs, and shows it there.
      const ids = libraryOrder(catalog.getState()).map((x) => x.id);
      void player.playList(ids, hit.id, { kind: 'songs', label: t('source.library') });
      navigate(`/songs?focus=${encodeURIComponent(hit.id)}`);
    } else navigate(`/${hit.kind}s/${hit.id}`);
  };

  const switchTab = (dir: 1 | -1) => {
    // Tabs with no matches are skipped; All always has them when anything matched.
    let i = TABS.indexOf(tab);
    for (let n = 0; n < TABS.length; n++) {
      i = (i + dir + TABS.length) % TABS.length;
      if (counts[TABS[i]!] > 0) break;
    }
    setTab(TABS[i]!);
    setActive(0);
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    } else if (e.key === 'Tab') {
      // Focus stays in the box.
      e.preventDefault();
    } else if (!typed || counts.all === 0) {
      return;
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActive((a) => Math.min(a + 1, hits.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      e.preventDefault();
      switchTab(e.key === 'ArrowRight' ? 1 : -1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const hit = hits[active];
      if (hit) open(hit);
    }
  };

  const meta = (h: SearchHit): ReactNode => {
    if (h.kind === 'song') return h.artist ? <Highlight text={h.artist} ranges={h.artistRanges} /> : t('common.noArtist');
    if (h.kind === 'group') return t('count.playlists', { n: h.count ?? 0 });
    return t('count.songs', { n: h.count ?? 0 });
  };
  const optionId = (i: number) => `${listId}-${i}`;

  return createPortal(
    <div className="scrim search-scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div role="dialog" aria-modal="true" aria-label={t('search.title')} className="search-box" onKeyDown={onKey}>
        <div className="search-in">
          <Icon name="search" size={22} />
          <input
            ref={inputRef}
            type="text"
            role="combobox"
            aria-expanded={typed && counts.all > 0}
            aria-controls={listId}
            aria-activedescendant={typed && hits[active] ? optionId(active) : undefined}
            aria-autocomplete="list"
            aria-label={t('search.title')}
            placeholder={t('search.placeholder')}
            value={q}
            spellCheck={false}
            autoComplete="off"
            onChange={(e) => setQ(e.target.value)}
            // ←→ belong to the tabs, so the caret stays at the end.
            onSelect={(e) => {
              const el = e.currentTarget;
              if (el.selectionStart === el.selectionEnd && el.selectionStart !== el.value.length) el.setSelectionRange(el.value.length, el.value.length);
            }}
          />
          <button type="button" className="kbd-btn" onClick={onClose}>
            Esc
          </button>
        </div>

        {typed && counts.all > 0 && (
          <>
            <div role="tablist" id={tabsId} aria-label={t('search.kinds')} className="search-tabs">
              {TABS.map((k) => (
                <button
                  key={k}
                  type="button"
                  role="tab"
                  tabIndex={-1}
                  aria-selected={tab === k}
                  aria-controls={listId}
                  className={'search-tab' + (tab === k ? ' on' : '') + (counts[k] === 0 ? ' dim' : '')}
                  disabled={counts[k] === 0}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => (setTab(k), setActive(0))}
                >
                  {t(TAB_LABEL[k])}
                  <span className="mono">{counts[k]}</span>
                </button>
              ))}
            </div>
            <ul ref={listRef} id={listId} role="listbox" aria-label={t(TAB_LABEL[tab])} className="search-list">
              {hits.map((h, i) => (
                <li
                  key={h.kind + h.id}
                  id={optionId(i)}
                  data-index={i}
                  role="option"
                  aria-selected={i === active}
                  className={'search-row' + (i === active ? ' active' : '')}
                  onMouseMove={() => i !== active && setActive(i)}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => open(h)}
                >
                  <span aria-hidden="true" className={'search-tile' + (h.kind === 'artist' ? ' round' : '')}>
                    {h.kind === 'artist' ? h.name.charAt(0).toUpperCase() : <Icon name={KIND_ICON[h.kind]} size={18} />}
                  </span>
                  <span className="search-tx">
                    <span className="ellipsis search-name">
                      <Highlight text={h.name} ranges={h.nameRanges} />
                    </span>
                    <span className="ellipsis search-meta">{meta(h)}</span>
                  </span>
                  {tab === 'all' && <span className="search-kind">{t(KIND_LABEL[h.kind])}</span>}
                  {i === active && (
                    <span aria-hidden="true" className="search-enter">
                      {h.kind === 'song' ? t('search.play') : t('search.open')} ↵
                    </span>
                  )}
                </li>
              ))}
            </ul>
            <div aria-hidden="true" className="search-foot">
              <span>
                <kbd>↑ ↓</kbd>
                {t('search.move')}
              </span>
              <span>
                <kbd>← →</kbd>
                {t('search.switchTab')}
              </span>
              <span>
                <kbd>↵</kbd>
                {t('search.open')}
              </span>
              <span>
                <kbd>Esc</kbd>
                {t('search.close')}
              </span>
            </div>
          </>
        )}
        {typed && counts.all === 0 && (
          <div className="search-empty">
            <strong>{t('search.nothing', { query: q.trim() })}</strong>
            <span>{t('search.nothingBody')}</span>
          </div>
        )}
        <p role="status" className="sr-only">
          {typed ? t('search.count', { n: counts.all }) : ''}
        </p>
      </div>
    </div>,
    document.body,
  );
}
