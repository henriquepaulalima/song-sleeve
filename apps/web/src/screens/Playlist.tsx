import { useRef, useState } from 'react';
import { Link, useLocation, useParams } from 'react-router';
import { formatLength, groupsOfPlaylist, playlistTracks, totalDurationMs, type Track } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useCatalogSelect, useCurrentTrackId, usePlayerValue } from '../app/hooks';
import { useI18n } from '../app/i18n';
import { player } from '../app/services';
import { tipFor } from '../app/shortcuts';
import { CollectionHeader, type Crumb } from '../components/CollectionHeader';
import { PlaylistMenu } from '../components/CollectionMenus';
import { EmptyState } from '../components/common';
import { Icon } from '../components/Icon';
import { Menu, MenuItem } from '../components/Menu';
import { ReorderableList } from '../components/ReorderableList';
import { PlaylistHero } from '../components/Sleeves';
import { SongRow } from '../components/SongRow';
import { SyncControl } from '../components/SyncControl';
import { SongTableHead } from './Songs';

/** designs/WebPlaylist: songs in playlist order, synced songs only on the web. */
export default function PlaylistPage() {
  const { t, locale } = useI18n();
  const { id = '' } = useParams();
  const { state } = useLocation();
  const actions = useActions();
  const s = useCatalog();
  const nowPlaying = useCurrentTrackId();
  const shuffle = usePlayerValue((p) => p.shuffle);
  const playlist = s.playlists.get(id);
  const tracks = useCatalogSelect((st) => playlistTracks(st, id), [id]);
  const groups = useCatalogSelect((st) => groupsOfPlaylist(st, id), [id]);
  const [menu, setMenu] = useState(false);
  const menuBtn = useRef<HTMLButtonElement>(null);

  if (!playlist) {
    return <EmptyState icon="playlist" title={t('playlist.notFound')} action={<Link to="/library" className="btn btn-secondary">{t('nav.library')}</Link>} />;
  }
  const ids = tracks.map((x) => x.id);
  // Play uses the player's shuffle mode; the shuffle button only toggles it.
  const play = (start: string | null) => void player.playList(ids, start, { kind: 'playlist', id: playlist.id, label: playlist.name });
  // Opened from a group, the breadcrumb goes back to that group.
  const fromGroup = s.groups.get((state as { fromGroup?: string } | null)?.fromGroup ?? '');
  const crumbs: Crumb[] = [
    { label: t('common.breadcrumbLibrary'), to: '/library' },
    fromGroup ? { label: fromGroup.name, to: `/groups/${fromGroup.id}` } : { label: t('library.chipPlaylists'), to: '/library?show=playlists' },
    { label: playlist.name },
  ];

  return (
    <>
      <CollectionHeader
        crumbs={crumbs}
        back={fromGroup ? `/groups/${fromGroup.id}` : '/library'}
        art={<PlaylistHero songs={tracks.length} />}
        kicker={t('playlist.kicker')}
        title={playlist.name}
        meta={
          <>
            <span>
              {t('count.songs', { n: tracks.length })}
              {tracks.length > 0 && ` · ${formatLength(locale, totalDurationMs(tracks))}`}
            </span>
            {groups.length > 0 && (
              <>
                <span aria-hidden="true">·</span>
                <span>{t('playlist.in')}</span>
                {groups.map((g) => (
                  <Link key={g.id} to={`/groups/${g.id}`} className="coll-pill">
                    {g.name}
                  </Link>
                ))}
              </>
            )}
          </>
        }
        end={
          <>
            <SyncControl spec={{ scope: 'playlist', id: playlist.id }} />
            <button ref={menuBtn} type="button" className="round-btn" aria-haspopup="menu" aria-expanded={menu} aria-label={t('playlist.options')} onClick={() => setMenu((m) => !m)} style={menu ? { background: 'var(--well)' } : undefined}>
              <Icon name="more" />
            </button>
            {menu && <PlaylistMenu playlist={playlist} anchor={menuBtn} onClose={() => setMenu(false)} />}
          </>
        }
      />

      <div className="coll-ctrls">
        <button type="button" className="btn btn-primary" disabled={tracks.length === 0} onClick={() => play(null)}>
          <Icon name="play" size={18} />
          {t('common.play')}
        </button>
        <button type="button" className={'round-btn' + (shuffle ? ' toggle-on' : '')} aria-label={t('player.shuffle')} aria-pressed={shuffle} data-tip={tipFor('shuffle', t)} onClick={() => player.setShuffle(!shuffle)}>
          <Icon name="shuffle" size={20} />
        </button>
        <AddToPlaylistButton playlistId={playlist.id} name={playlist.name} />
        {tracks.length > 1 && <span style={{ marginLeft: 'auto', fontSize: 13, color: 'var(--muted)' }}>{t('common.dragHint')}</span>}
      </div>

      {tracks.length === 0 ? (
        <EmptyState
          title={t('playlist.empty')}
          action={<AddToPlaylistButton playlistId={playlist.id} name={playlist.name} primary />}
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

/**
 * "Add songs" on a playlist: new files from this computer (the playlist already
 * checked in Add songs), or songs already in the library.
 */
function AddToPlaylistButton({ playlistId, name, primary }: { playlistId: string; name: string; primary?: boolean }) {
  const { t } = useI18n();
  const dialogs = useDialogs();
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  const go = (fn: () => void) => () => {
    setOpen(false);
    fn();
  };
  return (
    <>
      <button
        ref={btn}
        type="button"
        className={primary ? 'btn btn-primary' : 'btn btn-secondary btn-sm'}
        style={primary ? undefined : { minHeight: 44, ...(open ? { background: 'var(--hover)' } : null) }}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <Icon name="plus" size={16} />
        {t('playlist.addSongs')}
        <Icon name="chevronDown" size={16} />
      </button>
      {open && (
        <Menu anchor={btn} onClose={() => setOpen(false)} label={t('playlist.addSongs')} width={300} align="start">
          <MenuItem icon="file" label={t('playlist.addFiles')} sub={t('playlist.addFilesSub', { name })} onSelect={go(() => dialogs.open({ kind: 'addSongs', playlistIds: [playlistId] }))} />
          <MenuItem icon="playlistAdd" label={t('playlist.addExisting')} sub={t('playlist.addExistingSub')} onSelect={go(() => dialogs.open({ kind: 'addSongsToPlaylist', playlistId }))} />
        </Menu>
      )}
    </>
  );
}
