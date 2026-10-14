import { useCallback, useMemo } from 'react';
import type { Group, Playlist, Preferences, Track } from '@song-sleeve/core';
import { useT } from './i18n';
import { api, catalog, markDeletedHere } from './services';
import { useToasts } from './toasts';

/**
 * Server-data actions used from menus and lists. Each applies only the accepted
 * result (R-MUT-2) and reports connection_required / unknown results (R-MUT-1, 4).
 */
export function useActions() {
  const toasts = useToasts();
  const t = useT();

  const run = useCallback(
    async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
      try {
        return await fn();
      } catch (e) {
        toasts.error(e);
        // Refresh after a stale revision so the user sees the newer data (R-MUT-3).
        return undefined;
      }
    },
    [toasts],
  );

  return useMemo(
    () => ({
      async toggleInPlaylist(track: Track, playlist: Playlist) {
        const inIt = playlist.items.some((i) => i.track_id === track.id);
        const r = await run(() =>
          api.mutate<{ playlist: Playlist }>(inIt ? 'DELETE' : 'POST', `/me/playlists/${playlist.id}/songs`, { track_ids: [track.id], expected_revision: playlist.revision }),
        );
        if (r) catalog.upsertPlaylist(r.playlist);
      },
      async removeFromPlaylist(trackId: string, playlist: Playlist) {
        const r = await run(() => api.mutate<{ playlist: Playlist }>('DELETE', `/me/playlists/${playlist.id}/songs`, { track_ids: [trackId], expected_revision: playlist.revision }));
        if (r) catalog.upsertPlaylist(r.playlist);
      },
      async removeFromGroup(playlistId: string, group: Group) {
        const r = await run(() => api.mutate<{ group: Group }>('DELETE', `/me/groups/${group.id}/playlists`, { playlist_ids: [playlistId], expected_revision: group.revision }));
        if (r) catalog.upsertGroup(r.group);
      },
      /** Cancel sync, from any device: the sync fails everywhere (§7.8). */
      async cancelSync(track: Track) {
        const r = await run(() => api.mutate<{ deleted: boolean }>('DELETE', `/me/tracks/${track.id}?reason=cancel_pending`));
        if (r?.deleted) {
          markDeletedHere(track.id);
          catalog.removeTrack(track.id);
        }
      },
      async cancelOperation(track: Track) {
        if (!track.operation) return;
        const r = await run(() => api.mutate('POST', `/me/operations/${track.operation!.id}/cancel`));
        if (r) void import('./services').then((s) => s.catchUp());
      },
      async moveTrack(track: Track, afterId: string | null, beforeId: string | null) {
        const r = await run(() => api.mutate<{ track: Track }>('PUT', '/me/library/songs/order', { item_id: track.id, after_id: afterId, before_id: beforeId, expected_revision: track.revision }));
        if (r) catalog.upsertTrack(r.track);
        return !!r;
      },
      async movePlaylistSong(playlist: Playlist, trackId: string, afterId: string | null, beforeId: string | null) {
        const r = await run(() =>
          api.mutate<{ playlist: Playlist }>('PUT', `/me/playlists/${playlist.id}/songs/order`, { item_id: trackId, after_id: afterId, before_id: beforeId, expected_revision: playlist.revision }),
        );
        if (r) catalog.upsertPlaylist(r.playlist);
        return !!r;
      },
      async moveGroupPlaylist(group: Group, playlistId: string, afterId: string | null, beforeId: string | null) {
        const r = await run(() =>
          api.mutate<{ group: Group }>('PUT', `/me/groups/${group.id}/playlists/order`, { item_id: playlistId, after_id: afterId, before_id: beforeId, expected_revision: group.revision }),
        );
        if (r) catalog.upsertGroup(r.group);
        return !!r;
      },
      async setPreferences(p: Omit<Preferences, 'revision'>, current: Preferences) {
        const r = await run(() => api.mutate<{ preferences: Preferences }>('PUT', '/me/preferences/library-view', { ...p, expected_revision: current.revision }));
        if (r) catalog.setPreferences(r.preferences);
      },
      t,
    }),
    [run, t],
  );
}
