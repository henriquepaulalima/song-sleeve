import { useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';
import { groupPlaylists, libraryOrder, matchesQuery, playlistTracks } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useCatalogSelect } from '../app/hooks';
import { useT } from '../app/i18n';
import { player } from '../app/services';
import { EmptyState, SearchField } from '../components/common';
import { Icon } from '../components/Icon';

type Kind = 'songs' | 'playlists' | 'groups';

/**
 * designs/WebLibrary: card grid filtered by the saved type preference (R-ORG-7).
 * "See all" from Home opens it with one type selected for this visit only.
 */
export default function Library() {
  const t = useT();
  const dialogs = useDialogs();
  const actions = useActions();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [q, setQ] = useState('');
  const s = useCatalog();
  const prefs = s.preferences;
  const only = params.get('show') as Kind | null;
  const shown: Record<Kind, boolean> = only ? { songs: only === 'songs', playlists: only === 'playlists', groups: only === 'groups' } : { songs: prefs.songs, playlists: prefs.playlists, groups: prefs.groups };

  const counts = { songs: s.tracks.size, playlists: s.playlists.size, groups: s.groups.size };
  const order = useCatalogSelect((st) => libraryOrder(st));

  const toggle = (k: Kind) => {
    if (only) {
      // Leaving the one-type view returns to the saved filter.
      setParams({}, { replace: true });
      return;
    }
    const next = { ...shown, [k]: !shown[k] };
    if (!next.songs && !next.playlists && !next.groups) return; // at least one stays on
    void actions.setPreferences(next, prefs);
  };

  const cards = useMemo(() => {
    const out: { id: string; kind: Kind; name: string; meta: string; onOpen: () => void }[] = [];
    if (shown.groups) {
      for (const g of [...s.groups.values()].sort((a, b) => a.name.localeCompare(b.name))) {
        if (!matchesQuery(q, g.name)) continue;
        out.push({ id: g.id, kind: 'groups', name: g.name, meta: t('count.playlists', { n: groupPlaylists(s, g.id).length }), onOpen: () => navigate(`/groups/${g.id}`) });
      }
    }
    if (shown.playlists) {
      for (const p of [...s.playlists.values()].sort((a, b) => a.name.localeCompare(b.name))) {
        if (!matchesQuery(q, p.name)) continue;
        out.push({ id: p.id, kind: 'playlists', name: p.name, meta: t('count.songs', { n: playlistTracks(s, p.id).length }), onOpen: () => navigate(`/playlists/${p.id}`) });
      }
    }
    if (shown.songs) {
      const ids = order.map((x) => x.id);
      for (const tr of order) {
        const artist = tr.artist_id ? s.artists.get(tr.artist_id)?.name : undefined;
        if (!matchesQuery(q, tr.name, artist)) continue;
        out.push({ id: tr.id, kind: 'songs', name: tr.name, meta: artist ?? t('common.noArtist'), onOpen: () => void player.playList(ids, tr.id, { kind: 'library', label: t('source.library') }) });
      }
    }
    return out;
  }, [s, shown.groups, shown.playlists, shown.songs, q, order, t, navigate]);

  const chip = (k: Kind, label: string) => {
    const on = shown[k];
    return (
      <button
        key={k}
        type="button"
        className="chip"
        aria-pressed={on}
        onClick={() => toggle(k)}
        style={{ background: on ? 'var(--sand)' : 'transparent', color: on ? 'var(--ink)' : 'var(--white)', boxShadow: on ? 'none' : 'inset 0 0 0 1.5px rgba(255,255,255,.55)' }}
      >
        {on && <Icon name="check" size={16} strokeWidth={2.6} />}
        {label}
        <span className="mono" style={{ fontSize: 12, fontWeight: 500, opacity: 0.8 }}>
          {counts[k]}
        </span>
      </button>
    );
  };

  const kindLabel = { songs: t('kind.song'), playlists: t('kind.playlist'), groups: t('kind.group') };

  return (
    <>
      <header className="page-head">
        <div>
          <h1 className="page-title">{t('library.title')}</h1>
          <p style={{ margin: '6px 0 0', display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, color: 'var(--muted)' }}>
            <Icon name="cloudCheck" size={15} />
            {t('library.filterNote')}
          </p>
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          <button type="button" className="btn btn-secondary btn-sm" style={{ minHeight: 42 }} onClick={() => dialogs.open({ kind: 'createPlaylist' })}>
            <Icon name="plus" size={16} />
            {t('library.createPlaylist')}
          </button>
          <button type="button" className="btn btn-secondary btn-sm" style={{ minHeight: 42 }} onClick={() => dialogs.open({ kind: 'createGroup' })}>
            <Icon name="plus" size={16} />
            {t('library.createGroup')}
          </button>
        </div>
        <div role="group" aria-label={t('library.showTypes')} style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          {chip('songs', t('library.chipSongs'))}
          {chip('playlists', t('library.chipPlaylists'))}
          {chip('groups', t('library.chipGroups'))}
        </div>
      </header>
      <div style={{ marginTop: 18 }}>
        <SearchField label={t('library.search')} placeholder={t('library.searchPh')} value={q} onChange={setQ} maxWidth={460} />
      </div>

      {cards.length === 0 ? (
        q ? (
          <EmptyState title={t('state.noResults', { query: q })} body={t('state.noResultsBody')} />
        ) : counts.songs + counts.playlists + counts.groups === 0 ? (
          <EmptyState
            icon="note"
            title={t('state.noSongs')}
            body={t('state.noSongsBody')}
            action={
              <button type="button" className="btn btn-primary" onClick={() => dialogs.open({ kind: 'addSongs' })}>
                {t('home.addSongs')}
              </button>
            }
          />
        ) : (
          <EmptyState icon="library" title={t('library.nothingSelected')} body={t('library.nothingSelectedBody')} />
        )
      ) : (
        <div style={{ marginTop: 24, display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 16 }}>
          {cards.slice(0, 600).map((c) => (
            <CardLink key={c.kind + c.id} kind={c.kind} onOpen={c.onOpen} href={c.kind === 'songs' ? undefined : `/${c.kind}/${c.id}`}>
              <CardArt kind={c.kind} />
              <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0, padding: '0 4px 4px' }}>
                <span className="ellipsis" style={{ fontSize: 16, fontWeight: 600 }}>
                  {c.name}
                </span>
                <span className="ellipsis" style={{ fontSize: 13, color: 'var(--muted-2)' }}>
                  <strong style={{ fontWeight: 700, color: 'var(--white)', letterSpacing: '.04em', fontSize: 11, textTransform: 'uppercase' }}>{kindLabel[c.kind]}</strong> · {c.meta}
                </span>
              </span>
            </CardLink>
          ))}
        </div>
      )}
    </>
  );
}

function CardLink({ href, onOpen, children }: { kind: Kind; href?: string; onOpen: () => void; children: React.ReactNode }) {
  if (href) {
    return (
      <Link to={href} className="lcard">
        {children}
      </Link>
    );
  }
  return (
    <button type="button" className="lcard" onClick={onOpen}>
      {children}
    </button>
  );
}

function CardArt({ kind }: { kind: Kind }) {
  const vis = { position: 'relative' as const, aspectRatio: '4 / 3', borderRadius: 12 };
  if (kind === 'songs') {
    return (
      <span aria-hidden="true" style={{ ...vis, background: 'var(--well)', color: 'var(--sand)', display: 'flex', alignItems: 'center', justifyContent: 'center', boxShadow: 'inset 0 0 0 1px rgba(255,255,255,.08)' }}>
        <span style={{ position: 'absolute', inset: 9, borderRadius: 7, boxShadow: 'inset 0 0 0 1px rgba(255,255,255,.08)' }} />
        <Icon name="note" size={38} strokeWidth={1.8} />
      </span>
    );
  }
  if (kind === 'playlists') {
    return (
      <span aria-hidden="true" style={vis}>
        <span style={{ position: 'absolute', left: '14%', right: 0, top: 0, bottom: '14%', borderRadius: 12, background: 'var(--raised)' }} />
        <span style={{ position: 'absolute', left: 0, right: '14%', top: '14%', bottom: 0, borderRadius: 12, background: 'var(--well)', color: 'var(--sand)', display: 'flex', alignItems: 'center', justifyContent: 'center', boxShadow: 'inset 0 0 0 1px rgba(255,255,255,.1), 0 -6px 14px -6px rgba(0,0,0,.6)' }}>
          <Icon name="songs" size={34} />
        </span>
      </span>
    );
  }
  return (
    <span aria-hidden="true" style={{ ...vis, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <span style={{ position: 'relative', width: 120, height: 120 }}>
        <span style={{ position: 'absolute', left: 22, top: -4, width: 96, height: 96, borderRadius: 12, background: 'var(--raised-2)', transform: 'rotate(12deg)' }} />
        <span style={{ position: 'absolute', left: 12, top: 6, width: 96, height: 96, borderRadius: 12, background: 'var(--raised)', transform: 'rotate(5deg)' }} />
        <span style={{ position: 'absolute', left: 0, top: 20, width: 100, height: 100, borderRadius: 12, background: 'var(--well)', color: 'var(--sand)', display: 'flex', alignItems: 'center', justifyContent: 'center', boxShadow: 'inset 0 0 0 1px rgba(255,255,255,.1), 0 10px 18px -8px rgba(0,0,0,.6)' }}>
          <Icon name="group" size={32} />
        </span>
      </span>
    </span>
  );
}
