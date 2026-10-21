import { useRef, useState } from 'react';
import { Link, useParams } from 'react-router';
import { formatLength, groupPlaylists, groupQueue, playlistTracks, totalDurationMs, type Group, type Playlist } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useCatalogSelect, usePlayerValue } from '../app/hooks';
import { useI18n } from '../app/i18n';
import { player } from '../app/services';
import { tipFor } from '../app/shortcuts';
import { CollectionHeader } from '../components/CollectionHeader';
import { GroupMenu } from '../components/CollectionMenus';
import { EmptyState } from '../components/common';
import { Icon } from '../components/Icon';
import { Menu, MenuDivider, MenuItem, useContextMenu } from '../components/Menu';
import { ReorderableList } from '../components/ReorderableList';
import { GroupHero, PlaylistStack } from '../components/Sleeves';
import { SyncControl } from '../components/SyncControl';

const COLS = '44px minmax(0,2fr) 140px 120px 44px';

/** designs/WebGroup: playlists in group order; the web shows sync status only. */
export default function GroupPage() {
  const { t, locale } = useI18n();
  const { id = '' } = useParams();
  const dialogs = useDialogs();
  const actions = useActions();
  const s = useCatalog();
  const group = s.groups.get(id);
  const playlists = useCatalogSelect((st) => groupPlaylists(st, id), [id]);
  // Every playlist in group order, its songs in playlist order, each song once.
  const queue = useCatalogSelect((st) => groupQueue(st, id), [id]);
  const songCount = queue.length;
  const shuffle = usePlayerValue((p) => p.shuffle);
  const [menu, setMenu] = useState(false);
  const menuBtn = useRef<HTMLButtonElement>(null);

  if (!group) {
    return <EmptyState icon="group" title={t('group.notFound')} action={<Link to="/library" className="btn btn-secondary">{t('nav.library')}</Link>} />;
  }

  return (
    <>
      <CollectionHeader
        crumbs={[{ label: t('common.breadcrumbLibrary'), to: '/library' }, { label: t('library.chipGroups'), to: '/library?show=groups' }, { label: group.name }]}
        back="/library"
        art={<GroupHero label={t('count.playlists', { n: playlists.length })} />}
        kicker={t('group.kicker')}
        title={group.name}
        meta={
          <span>
            {t('count.playlists', { n: playlists.length })} · {t('count.songs', { n: songCount })}
          </span>
        }
        end={
          <>
            <SyncControl spec={{ scope: 'group', id: group.id }} />
            <button ref={menuBtn} type="button" className="round-btn" aria-haspopup="menu" aria-expanded={menu} aria-label={t('group.options')} onClick={() => setMenu((m) => !m)} style={menu ? { background: 'var(--well)' } : undefined}>
              <Icon name="more" />
            </button>
            {menu && <GroupMenu group={group} anchor={menuBtn} onClose={() => setMenu(false)} />}
          </>
        }
      />

      <div className="coll-ctrls">
        <button type="button" className="btn btn-primary" disabled={queue.length === 0} onClick={() => void player.playList(queue, null, { kind: 'group', id: group.id, label: group.name })}>
          <Icon name="play" size={18} />
          {t('common.play')}
        </button>
        <button type="button" className={'round-btn' + (shuffle ? ' toggle-on' : '')} aria-label={t('player.shuffle')} aria-pressed={shuffle} data-tip={tipFor('shuffle', t)} onClick={() => player.setShuffle(!shuffle)}>
          <Icon name="shuffle" size={20} />
        </button>
        <button type="button" className="btn btn-secondary btn-sm" style={{ minHeight: 44 }} onClick={() => dialogs.open({ kind: 'addPlaylistsToGroup', groupId: group.id })}>
          <Icon name="plus" size={16} />
          {t('group.addPlaylists')}
        </button>
        {playlists.length > 1 && <span style={{ marginLeft: 'auto', fontSize: 13, color: 'var(--muted)' }}>{t('common.dragHint')}</span>}
      </div>

      <h2 style={{ margin: '30px 0 0', fontSize: 13, fontWeight: 600, letterSpacing: '.06em', color: 'var(--muted)' }}>{t('group.playlistsIn')}</h2>

      {playlists.length === 0 ? (
        <EmptyState
          title={t('group.empty')}
          action={
            <button type="button" className="btn btn-secondary" onClick={() => dialogs.open({ kind: 'addPlaylistsToGroup', groupId: group.id })}>
              {t('group.addPlaylists')}
            </button>
          }
        />
      ) : (
        <div className="table-scroll" style={{ marginTop: 12, paddingBottom: 40 }}>
          <div style={{ minWidth: 620, display: 'flex', flexDirection: 'column', gap: 8 }}>
            <div aria-hidden="true" style={{ display: 'grid', gridTemplateColumns: COLS, columnGap: 14 }}>
              <span />
              <span className="th" style={{ padding: '0 4px 6px' }}>
                {t('group.colPlaylist')}
              </span>
              <span className="th" style={{ padding: '0 4px 6px' }}>
                {t('group.colSongs')}
              </span>
              <span className="th" style={{ padding: '0 4px 6px' }}>
                {t('group.colLength')}
              </span>
              <span />
            </div>
            <ReorderableList<Playlist>
              items={playlists}
              getId={(p) => p.id}
              getName={(p) => p.name}
              enabled
              label={t('group.listLabel', { name: group.name })}
              className="song-list"
              onMove={(p, after, before) => actions.moveGroupPlaylist(group, p.id, after, before)}
              renderRow={(p, h) => <GroupRow p={p} group={group} grip={h.grip} locale={locale} />}
            />
          </div>
        </div>
      )}
    </>
  );
}

function GroupRow({ p, group, grip, locale }: { p: Playlist; group: Group; grip: React.ReactNode; locale: 'en' | 'pt-BR' }) {
  const { t } = useI18n();
  const dialogs = useDialogs();
  const actions = useActions();
  const tracks = useCatalogSelect((st) => playlistTracks(st, p.id), [p.id]);
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  // Right-click opens the same in-group menu as the ⋯ button.
  const context = useContextMenu();
  const menuItems = (close: () => void) => (
    <>
      <MenuItem icon="pencil" label={t('group.renamePlaylist')} onSelect={() => (close(), dialogs.open({ kind: 'renamePlaylist', id: p.id }))} />
      <MenuItem icon="minus" label={t('group.removeFromGroup')} onSelect={() => (close(), void actions.removeFromGroup(p.id, group))} />
      <MenuDivider />
      <MenuItem icon="trash" label={t('group.deletePlaylist')} sub={t('group.deletePlaylistSub')} danger onSelect={() => (close(), dialogs.open({ kind: 'deletePlaylist', id: p.id, fromGroupId: group.id }))} />
    </>
  );
  return (
    <div onContextMenu={context.onContextMenu} style={{ display: 'grid', gridTemplateColumns: COLS, alignItems: 'center', columnGap: 14, minHeight: 76, padding: '0 6px', borderRadius: 16, background: 'var(--charcoal)' }}>
      {grip}
      <Link to={`/playlists/${p.id}`} state={{ fromGroup: group.id }} style={{ display: 'flex', alignItems: 'center', gap: 14, minWidth: 0, color: 'var(--white)', textDecoration: 'none', fontWeight: 600, fontSize: 16 }}>
        <PlaylistStack size={52} songs={tracks.length} />
        <span className="ellipsis">{p.name}</span>
      </Link>
      <span className="mono" style={{ fontSize: 13, color: 'var(--muted)' }}>
        {tracks.length}
      </span>
      <span className="mono" style={{ fontSize: 13, color: 'var(--muted)' }}>
        {formatLength(locale, totalDurationMs(tracks))}
      </span>
      <button ref={btn} type="button" className="icon-btn" aria-haspopup="menu" aria-expanded={open} aria-label={t('common.moreActions', { name: p.name })} onClick={() => setOpen((o) => !o)}>
        <Icon name="more" />
      </button>
      {open && (
        <Menu anchor={btn} onClose={() => setOpen(false)} label={t('common.moreActions', { name: p.name })} width={260}>
          {menuItems(() => setOpen(false))}
        </Menu>
      )}
      {context.at && (
        <Menu anchor={context.at} onClose={context.close} label={t('common.moreActions', { name: p.name })} width={260}>
          {menuItems(context.close)}
        </Menu>
      )}
    </div>
  );
}
