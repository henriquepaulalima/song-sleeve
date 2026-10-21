import type { MouseEvent as ReactMouseEvent, ReactNode } from 'react';
import { groupQueue, playlistTracks, type Group, type Playlist, type Track } from '@song-sleeve/core';
import { useDialogs } from '../app/dialogs';
import { useT } from '../app/i18n';
import { catalog, player } from '../app/services';
import { Menu, MenuDivider, MenuItem, useContextMenu, type MenuAnchor } from './Menu';
import { SongMenu } from './SongRow';

/** Plays a playlist from the top, in the player's shuffle mode. */
export function playPlaylist(p: Playlist): void {
  const ids = playlistTracks(catalog.getState(), p.id).map((x) => x.id);
  if (ids.length) void player.playList(ids, null, { kind: 'playlist', id: p.id, label: p.name });
}

/** Plays every song of a group's playlists, each once. */
export function playGroup(g: Group): void {
  const ids = groupQueue(catalog.getState(), g.id);
  if (ids.length) void player.playList(ids, null, { kind: 'group', id: g.id, label: g.name });
}

/**
 * A playlist's own menu: the page's ⋯ button and right-click on its card or
 * sidebar link. Right-click menus start with Play.
 */
export function PlaylistMenu({ playlist, anchor, onClose, withPlay, align }: { playlist: Playlist; anchor: MenuAnchor; onClose: () => void; withPlay?: boolean; align?: 'start' | 'end' }) {
  const t = useT();
  const dialogs = useDialogs();
  const go = (fn: () => void) => () => {
    onClose();
    fn();
  };
  return (
    <Menu anchor={anchor} onClose={onClose} label={t('playlist.options')} width={260} align={align}>
      {withPlay && <MenuItem icon="play" label={t('common.play')} onSelect={go(() => playPlaylist(playlist))} />}
      <MenuItem icon="pencil" label={t('playlist.rename')} onSelect={go(() => dialogs.open({ kind: 'renamePlaylist', id: playlist.id }))} />
      <MenuItem icon="folderPlus" label={t('playlist.addToGroups')} onSelect={go(() => dialogs.open({ kind: 'addToGroups', playlistId: playlist.id }))} />
      <MenuDivider />
      <MenuItem icon="trash" label={t('playlist.delete')} sub={t('playlist.deleteSub')} danger onSelect={go(() => dialogs.open({ kind: 'deletePlaylist', id: playlist.id }))} />
    </Menu>
  );
}

/** A group's own menu: the page's ⋯ button and right-click on its card or sidebar link. */
export function GroupMenu({ group, anchor, onClose, withPlay }: { group: Group; anchor: MenuAnchor; onClose: () => void; withPlay?: boolean }) {
  const t = useT();
  const dialogs = useDialogs();
  const go = (fn: () => void) => () => {
    onClose();
    fn();
  };
  return (
    <Menu anchor={anchor} onClose={onClose} label={t('group.options')} width={260}>
      {withPlay && <MenuItem icon="play" label={t('common.play')} onSelect={go(() => playGroup(group))} />}
      <MenuItem icon="pencil" label={t('group.rename')} onSelect={go(() => dialogs.open({ kind: 'renameGroup', id: group.id }))} />
      <MenuDivider />
      <MenuItem icon="trash" label={t('group.delete')} sub={t('group.deleteSub')} danger onSelect={go(() => dialogs.open({ kind: 'deleteGroup', id: group.id }))} />
    </Menu>
  );
}

export type ContextItem = { kind: 'playlist'; playlist: Playlist } | { kind: 'group'; group: Group } | { kind: 'song'; track: Track; onPlay: () => void };

/**
 * Gives a card or link a right-click menu: its children receive the handler to
 * put on their root element. The menu opens at the pointer and starts with Play.
 */
export function WithContextMenu({ item, children }: { item: ContextItem; children: (onContextMenu: (e: ReactMouseEvent) => void) => ReactNode }) {
  const { at, onContextMenu, close } = useContextMenu();
  return (
    <>
      {children(onContextMenu)}
      {at && item.kind === 'playlist' && <PlaylistMenu playlist={item.playlist} anchor={at} onClose={close} withPlay />}
      {at && item.kind === 'group' && <GroupMenu group={item.group} anchor={at} onClose={close} withPlay />}
      {at && item.kind === 'song' && <SongMenu track={item.track} ctx={{}} anchor={at} onClose={close} onPlay={item.onPlay} />}
    </>
  );
}
