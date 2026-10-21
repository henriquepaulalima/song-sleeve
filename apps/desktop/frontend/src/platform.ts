/** Desktop platform hooks; replaces the web's app/platform.ts in this build. */
import type { Track } from '@song-sleeve/core';
import type { LocalSongActions, Platform, Translate } from '@web/app/platform';
import { local } from './bridge';
import { catchUp } from './services';

export type { LocalSongActions, Platform, Translate } from '@web/app/platform';

interface Candidate {
  path: string;
  name: string;
  artist_hint: string;
  format: string;
  duration_ms: number;
  byte_size: number;
  sha256: string;
  problem?: string;
}

async function pickOne(title: string): Promise<string | null> {
  const { paths } = await local<{ paths: string[] | null }>('POST', '/pick', { folder: false, title });
  return paths?.[0] ?? null;
}

function artistName(track: Track): Promise<string | null> {
  // The current artist stays; the catalog has its name.
  return import('./services').then(({ catalog }) => {
    const id = track.artist_id;
    return id ? (catalog.getState().artists.get(id)?.name ?? null) : null;
  });
}

async function replaceWith(track: Track, c: Candidate, t: Translate) {
  if (c.problem) throw new Error(c.problem === 'in_song_sleeve_folder' ? t('add.inSleeve') : t(`limit.${c.problem}` as never));
  await local('POST', '/replace-source', { id: track.id, candidate: c, name: track.name, artist: await artistName(track) });
}

const actions: LocalSongActions = {
  async sync(ids) {
    await local('POST', '/sync', { ids });
    await catchUp();
  },
  async retry(ids) {
    await local('POST', '/sync', { ids, retry: true });
    await catchUp();
  },
  async cancelQueued(ids) {
    await local('POST', '/sync', { ids, cancel: true });
    await catchUp();
  },
  async relink(track, t) {
    const path = await pickOne(t('relink.pickTitle', { name: track.name }));
    if (!path) return;
    try {
      await local('POST', '/relink', { id: track.id, path });
    } catch (e) {
      const details = (e as { details?: { candidate?: Candidate } }).details;
      if (!details?.candidate) throw e;
      // A different file: ask before using it as the song's audio (§7.6).
      if (!window.confirm(`${t('relink.different')}\n\n${t('relink.differentBody')}`)) return;
      await replaceWith(track, details.candidate, t);
    }
  },
  async replaceSource(track, t) {
    const path = track.device?.health === 'source_changed' && track.device.source_path ? track.device.source_path : await pickOne(t('menu.replace'));
    if (!path) return;
    const { candidates } = await local<{ candidates: Candidate[] }>('POST', '/analyze', { paths: [path] });
    const c = candidates[0];
    if (!c) throw new Error(t('add.unreadable'));
    await replaceWith(track, c, t);
  },
  async showInFolder(track) {
    await local('POST', '/reveal', { id: track.id });
  },
};

export const platform: Platform = {
  kind: 'desktop',
  local: actions,
  async logoutAndRemove() {
    await local('POST', '/logout', { remove: true });
  },
  nav: [{ to: '/settings', icon: 'settings', label: 'nav.settings' }],
};
