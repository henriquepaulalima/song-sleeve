import { useRef, useState } from 'react';
import { Link, useParams } from 'react-router';
import { formatLength, groupPlaylists, playlistTracks, totalDurationMs, type Group, type Playlist } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useCatalogSelect } from '../app/hooks';
import { useI18n } from '../app/i18n';
import { EmptyState } from '../components/common';
import { Icon } from '../components/Icon';
import { Menu, MenuDivider, MenuItem } from '../components/Menu';
import { ReorderableList } from '../components/ReorderableList';
import { GroupHero, PlaylistStack } from '../components/Sleeves';

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
  const songCount = useCatalogSelect((st) => new Set(groupPlaylists(st, id).flatMap((p) => p.items.map((i) => i.track_id))).size, [id]);
  const [menu, setMenu] = useState(false);
  const menuBtn = useRef<HTMLButtonElement>(null);

  if (!group) {
    return <EmptyState icon="group" title={t('group.notFound')} action={<Link to="/library" className="btn btn-secondary">{t('nav.library')}</Link>} />;
  }

  return (
    <>
      <nav aria-label="Breadcrumb" style={{ fontSize: 13, color: 'var(--muted)' }}>
        <Link to="/library" style={{ color: 'var(--white)' }}>
          {t('common.breadcrumbLibrary')}
        </Link>{' '}
        <span aria-hidden="true">/</span> {t('library.chipGroups')}
      </nav>
      <header style={{ marginTop: 16, display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', gap: 24 }}>
        <GroupHero />
        <div style={{ flex: '1 1 300px', minWidth: 0 }}>
          <div className="kicker">{t('group.kicker')}</div>
          <h1 className="page-title" style={{ margin: '6px 0 0', fontSize: 44, overflowWrap: 'anywhere' }}>
            {group.name}
          </h1>
          <p style={{ margin: '10px 0 0', fontSize: 14, color: 'var(--muted)' }}>
            {t('count.playlists', { n: playlists.length })} · {t('count.songs', { n: songCount })}
          </p>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '10px 16px 10px 10px', borderRadius: 16, background: 'var(--charcoal)' }}>
            <span style={{ width: 40, height: 40, borderRadius: 12, background: 'var(--well)', color: 'var(--sand)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <Icon name="cloudCheck" size={20} />
            </span>
            <span style={{ fontSize: 14, fontWeight: 600 }}>{t('playlist.cloud')}</span>
          </span>
          <button ref={menuBtn} type="button" className="round-btn" aria-haspopup="menu" aria-expanded={menu} aria-label={t('group.options')} onClick={() => setMenu((m) => !m)}>
            <Icon name="more" />
          </button>
          {menu && (
            <Menu anchor={menuBtn} onClose={() => setMenu(false)} label={t('group.options')} width={260}>
              <MenuItem label={t('group.rename')} onSelect={() => (setMenu(false), dialogs.open({ kind: 'renameGroup', id: group.id }))} />
              <MenuDivider />
              <MenuItem label={t('group.delete')} sub={t('group.deleteSub')} danger onSelect={() => (setMenu(false), dialogs.open({ kind: 'deleteGroup', id: group.id }))} />
            </Menu>
          )}
        </div>
      </header>

      <div style={{ marginTop: 30, display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
        <h2 style={{ margin: 0, fontSize: 13, fontWeight: 600, letterSpacing: '.06em', color: 'var(--muted)' }}>{t('group.playlistsIn')}</h2>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => dialogs.open({ kind: 'addPlaylistsToGroup', groupId: group.id })}>
          <Icon name="plus" size={16} />
          {t('group.addPlaylists')}
        </button>
      </div>

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
  return (
    <div style={{ display: 'grid', gridTemplateColumns: COLS, alignItems: 'center', columnGap: 14, minHeight: 76, padding: '0 6px', borderRadius: 16, background: 'var(--charcoal)' }}>
      {grip}
      <Link to={`/playlists/${p.id}`} style={{ display: 'flex', alignItems: 'center', gap: 14, minWidth: 0, color: 'var(--white)', textDecoration: 'none', fontWeight: 600, fontSize: 16 }}>
        <PlaylistStack size={58} inner={48} />
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
          <MenuItem label={t('group.renamePlaylist')} onSelect={() => (setOpen(false), dialogs.open({ kind: 'renamePlaylist', id: p.id }))} />
          <MenuItem label={t('group.removeFromGroup')} onSelect={() => (setOpen(false), void actions.removeFromGroup(p.id, group))} />
          <MenuDivider />
          <MenuItem label={t('group.deletePlaylist')} sub={t('group.deletePlaylistSub')} danger onSelect={() => (setOpen(false), dialogs.open({ kind: 'deletePlaylist', id: p.id, fromGroupId: group.id }))} />
        </Menu>
      )}
    </div>
  );
}
