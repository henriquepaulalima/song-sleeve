import { useRef, useState } from 'react';
import { Link, useParams } from 'react-router';
import { groupsOfPlaylist, playlistTracks, type Track } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useCatalogSelect, usePlayer } from '../app/hooks';
import { useT } from '../app/i18n';
import { player } from '../app/services';
import { EmptyState } from '../components/common';
import { Icon } from '../components/Icon';
import { Menu, MenuDivider, MenuItem } from '../components/Menu';
import { ReorderableList } from '../components/ReorderableList';
import { PlaylistHero } from '../components/Sleeves';
import { SongRow } from '../components/SongRow';
import { SongTableHead } from './Songs';

/** designs/WebPlaylist: songs in playlist order, synced songs only on the web. */
export default function PlaylistPage() {
  const t = useT();
  const { id = '' } = useParams();
  const dialogs = useDialogs();
  const actions = useActions();
  const s = useCatalog();
  const nowPlaying = usePlayer().current?.id;
  const playlist = s.playlists.get(id);
  const tracks = useCatalogSelect((st) => playlistTracks(st, id), [id]);
  const groups = useCatalogSelect((st) => groupsOfPlaylist(st, id), [id]);
  const [menu, setMenu] = useState(false);
  const menuBtn = useRef<HTMLButtonElement>(null);

  if (!playlist) {
    return <EmptyState icon="songs" title={t('playlist.notFound')} action={<Link to="/library" className="btn btn-secondary">{t('nav.library')}</Link>} />;
  }
  const ids = tracks.map((x) => x.id);
  const play = (start: string | null, shuffle?: boolean) => void player.playList(ids, start, { kind: 'playlist', id: playlist.id, label: playlist.name }, shuffle === undefined ? {} : { shuffle });

  return (
    <>
      <nav aria-label="Breadcrumb" style={{ fontSize: 13, color: 'var(--muted)' }}>
        <Link to="/library" style={{ color: 'var(--white)' }}>
          {t('common.breadcrumbLibrary')}
        </Link>{' '}
        <span aria-hidden="true">/</span> {t('library.chipPlaylists')}
      </nav>
      <header style={{ marginTop: 16, display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', gap: 24 }}>
        <PlaylistHero />
        <div style={{ flex: '1 1 320px', minWidth: 0 }}>
          <div className="kicker">{t('playlist.kicker')}</div>
          <h1 className="page-title" style={{ margin: '6px 0 0', fontSize: 44, overflowWrap: 'anywhere' }}>
            {playlist.name}
          </h1>
          <div style={{ marginTop: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, fontSize: 14, color: 'var(--muted)' }}>
            <span>{t('count.songs', { n: tracks.length })}</span>
            {groups.length > 0 && (
              <>
                <span aria-hidden="true">·</span>
                <span>{t('playlist.in')}</span>
                {groups.map((g) => (
                  <Link key={g.id} to={`/groups/${g.id}`} style={{ display: 'inline-flex', alignItems: 'center', minHeight: 32, padding: '0 12px', borderRadius: 999, background: 'var(--charcoal)', color: 'var(--white)', fontSize: 13, textDecoration: 'none' }}>
                    {g.name}
                  </Link>
                ))}
              </>
            )}
          </div>
        </div>
      </header>

      <div style={{ marginTop: 22, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
        <button type="button" className="btn btn-primary" disabled={tracks.length === 0} onClick={() => play(null, false)}>
          <Icon name="play" size={18} />
          {t('common.play')}
        </button>
        <button type="button" className="round-btn" aria-label={t('common.shufflePlay')} disabled={tracks.length === 0} onClick={() => play(null, true)}>
          <Icon name="shuffle" size={20} />
        </button>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, minHeight: 44, padding: '0 14px', borderRadius: 999, background: 'var(--charcoal)', fontSize: 14, fontWeight: 600, boxShadow: 'inset 0 0 0 1.5px var(--sand)' }}>
          <Icon name="cloudCheck" size={18} style={{ color: 'var(--sand)' }} />
          {t('playlist.cloud')}
        </span>
        <button type="button" className="btn btn-secondary btn-sm" style={{ minHeight: 44 }} onClick={() => dialogs.open({ kind: 'addSongsToPlaylist', playlistId: playlist.id })}>
          <Icon name="plus" size={16} />
          {t('playlist.addSongs')}
        </button>
        <button ref={menuBtn} type="button" className="round-btn" aria-haspopup="menu" aria-expanded={menu} aria-label={t('playlist.options')} onClick={() => setMenu((m) => !m)} style={menu ? { background: 'var(--well)' } : undefined}>
          <Icon name="more" />
        </button>
        {menu && (
          <Menu anchor={menuBtn} onClose={() => setMenu(false)} label={t('playlist.options')} width={260} align="start">
            <MenuItem label={t('playlist.rename')} onSelect={() => (setMenu(false), dialogs.open({ kind: 'renamePlaylist', id: playlist.id }))} />
            <MenuItem label={t('playlist.addToGroups')} onSelect={() => (setMenu(false), dialogs.open({ kind: 'addToGroups', playlistId: playlist.id }))} />
            <MenuDivider />
            <MenuItem label={t('playlist.delete')} sub={t('playlist.deleteSub')} danger onSelect={() => (setMenu(false), dialogs.open({ kind: 'deletePlaylist', id: playlist.id }))} />
          </Menu>
        )}
        {tracks.length > 1 && <span style={{ marginLeft: 'auto', fontSize: 13, color: 'var(--muted)' }}>{t('common.dragHint')}</span>}
      </div>

      {tracks.length === 0 ? (
        <EmptyState
          title={t('playlist.empty')}
          action={
            <button type="button" className="btn btn-primary" onClick={() => dialogs.open({ kind: 'addSongsToPlaylist', playlistId: playlist.id })}>
              {t('playlist.addSongs')}
            </button>
          }
        />
      ) : (
        <div className="table-scroll" style={{ marginTop: 22 }}>
          <div style={{ minWidth: 820, display: 'flex', flexDirection: 'column', gap: 6 }}>
            <SongTableHead grip artist />
            <ReorderableList<Track>
              items={tracks}
              getId={(x) => x.id}
              getName={(x) => x.name}
              enabled
              label={t('playlist.listLabel', { name: playlist.name })}
              onMove={(x, after, before) => actions.movePlaylistSong(playlist, x.id, after, before)}
              renderRow={(x, h) => (
                <SongRow
                  track={x}
                  playing={x.id === nowPlaying}
                  onPlay={() => play(x.id)}
                  ctx={{ grip: h.grip, playlist, moveToTop: h.moveToTop, moveToBottom: h.moveToBottom }}
                  dragging={h.dragging}
                />
              )}
            />
          </div>
        </div>
      )}
    </>
  );
}
