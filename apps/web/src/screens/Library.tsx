import { useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';
import { groupPlaylists, libraryOrder, playlistTracks, type Group, type Playlist, type Track } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useCatalogSelect, useCurrentTrackId, useIsPlaying } from '../app/hooks';
import { useT } from '../app/i18n';
import { player } from '../app/services';
import { EmptyState } from '../components/common';
import { Menu, MenuItem } from '../components/Menu';
import { Icon, type IconName } from '../components/Icon';
import { Equalizer, GroupArt, PlaylistArt } from '../components/Sleeves';
import { WithContextMenu, type ContextItem } from '../components/CollectionMenus';
import { AddSongsButton, PageHeader } from '../components/PageHeader';
import { SyncControl } from '../components/SyncControl';

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
  const s = useCatalog();
  const prefs = s.preferences;
  const only = params.get('show') as Kind | null;
  const shown: Record<Kind, boolean> = only ? { songs: only === 'songs', playlists: only === 'playlists', groups: only === 'groups' } : { songs: prefs.songs, playlists: prefs.playlists, groups: prefs.groups };

  const counts = { songs: s.tracks.size, playlists: s.playlists.size, groups: s.groups.size };
  const order = useCatalogSelect((st) => libraryOrder(st));
  const nowPlaying = useCurrentTrackId();

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
    const out: { id: string; kind: Kind; name: string; meta: string; count: number; onOpen: () => void; item: ContextItem }[] = [];
    if (shown.groups) {
      for (const g of [...s.groups.values()].sort((a, b) => a.name.localeCompare(b.name))) {
        const n = groupPlaylists(s, g.id).length;
        out.push({ id: g.id, kind: 'groups', name: g.name, meta: t('count.playlists', { n }), count: n, onOpen: () => navigate(`/groups/${g.id}`), item: { kind: 'group', group: g as Group } });
      }
    }
    if (shown.playlists) {
      for (const p of [...s.playlists.values()].sort((a, b) => a.name.localeCompare(b.name))) {
        const n = playlistTracks(s, p.id).length;
        out.push({ id: p.id, kind: 'playlists', name: p.name, meta: t('count.songs', { n }), count: n, onOpen: () => navigate(`/playlists/${p.id}`), item: { kind: 'playlist', playlist: p as Playlist } });
      }
    }
    if (shown.songs) {
      const ids = order.map((x) => x.id);
      for (const tr of order) {
        const artist = tr.artist_id ? s.artists.get(tr.artist_id)?.name : undefined;
        const play = () => void player.playList(ids, tr.id, { kind: 'library', label: t('nav.library') });
        out.push({ id: tr.id, kind: 'songs', name: tr.name, meta: artist ?? t('common.noArtist'), count: 0, onOpen: play, item: { kind: 'song', track: tr as Track, onPlay: play } });
      }
    }
    return out;
  }, [s, shown.groups, shown.playlists, shown.songs, order, t, navigate]);

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
      <PageHeader
        title={t('library.title')}
        sub={[t('count.songs', { n: counts.songs }), t('count.playlists', { n: counts.playlists }), t('count.groups', { n: counts.groups })].join(' · ')}
        actions={
          <>
            <SyncControl spec={{ scope: 'library' }} />
            <AddSongsButton />
          </>
        }
      />
      <div className="page-toolbar">
        <div role="group" aria-label={t('library.showTypes')} data-tip={t('library.filterNote')} style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          {chip('songs', t('library.chipSongs'))}
          {chip('playlists', t('library.chipPlaylists'))}
          {chip('groups', t('library.chipGroups'))}
        </div>
        <CreateButton />
      </div>

      {cards.length === 0 ? (
        counts.songs + counts.playlists + counts.groups === 0 ? (
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
            <CardLink key={c.kind + c.id} kind={c.kind} item={c.item} current={c.kind === 'songs' && c.id === nowPlaying} onOpen={c.onOpen} href={c.kind === 'songs' ? undefined : `/${c.kind}/${c.id}`}>
              <CardArt kind={c.kind} count={c.count} meta={c.meta} current={c.kind === 'songs' && c.id === nowPlaying} />
              <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0, padding: '0 4px 4px' }}>
                <span className="ellipsis" style={{ fontSize: 16, fontWeight: 600 }}>
                  {c.name}
                </span>
                <span className="ellipsis" style={{ fontSize: 13, color: 'var(--muted-2)' }}>
                  <Icon name={kindIcon[c.kind]} size={13} style={{ color: 'var(--sand)', verticalAlign: '-2px', marginRight: 5 }} />
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

/** A card; right-click opens the item's own menu. */
function CardLink({ href, onOpen, current, item, children }: { kind: Kind; href?: string; current?: boolean; onOpen: () => void; item: ContextItem; children: React.ReactNode }) {
  return (
    <WithContextMenu item={item}>
      {(onContextMenu) =>
        href ? (
          <Link to={href} className="lcard" onContextMenu={onContextMenu}>
            {children}
          </Link>
        ) : (
          // The song that's playing pauses instead of restarting.
          <button type="button" className={'lcard' + (current ? ' current' : '')} onClick={current ? () => void player.toggle() : onOpen} onContextMenu={onContextMenu}>
            {children}
          </button>
        )
      }
    </WithContextMenu>
  );
}

const kindIcon: Record<Kind, IconName> = { songs: 'note', playlists: 'playlist', groups: 'group' };

/** Card art per kind (designs/proposals/LibraryCards): sleeve, 2×2 mosaic, folder. */
function CardArt({ kind, count, meta, current }: { kind: Kind; count: number; meta: string; current: boolean }) {
  if (kind === 'songs') return <SongCardArt current={current} />;
  if (kind === 'playlists') return <PlaylistArt width="100%" height="auto" songs={count} style={{ aspectRatio: '4 / 3', gap: 5 }} />;
  return (
    <span aria-hidden="true" style={{ aspectRatio: '4 / 3', display: 'flex', alignItems: 'flex-end', padding: '4% 3% 0' }}>
      <GroupArt width="100%" height="100%" label={meta} />
    </span>
  );
}

function SongCardArt({ current }: { current: boolean }) {
  return (
    <span aria-hidden="true" className={'song-art' + (current ? ' current' : '')}>
      {current ? <NowPlaying /> : <Icon name="note" size={38} strokeWidth={1.8} />}
    </span>
  );
}

function NowPlaying() {
  return <Equalizer playing={useIsPlaying()} size={34} />;
}

/** One round + with a menu: New playlist, New group (designs/proposals/Round3 #create). */
function CreateButton() {
  const t = useT();
  const dialogs = useDialogs();
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  const go = (kind: 'createPlaylist' | 'createGroup') => () => {
    setOpen(false);
    dialogs.open({ kind });
  };
  return (
    <>
      <button
        ref={btn}
        type="button"
        className={'round-btn create-btn' + (open ? ' open' : '')}
        style={{ marginLeft: 'auto' }}
        aria-label={t('library.create')}
        aria-haspopup="menu"
        aria-expanded={open}
        data-tip={t('library.create')}
        data-tip-align="end"
        onClick={() => setOpen((o) => !o)}
      >
        <Icon name="plus" size={22} />
      </button>
      {open && (
        <Menu anchor={btn} onClose={() => setOpen(false)} label={t('library.create')} width={230}>
          <MenuItem icon="playlist" label={t('library.newPlaylist')} onSelect={go('createPlaylist')} />
          <MenuItem icon="group" label={t('library.newGroup')} onSelect={go('createGroup')} />
        </Menu>
      )}
    </>
  );
}
