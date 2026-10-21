/**
 * Sync scopes (§7.4): a library, playlist or group whose songs installed devices
 * upload now and whenever more join (R-CLOUD-1). Shared by the page switch and
 * the details dialog so both read and change the same state.
 */
import { useState } from 'react';
import { formatBytes, groupPlaylists, libraryOrder, playlistTracks, songStatus, type CatalogState, type Scope, type Track } from '@song-sleeve/core';
import { useCatalog } from './hooks';
import { useI18n } from './i18n';
import { platform } from './platform';
import { api, catalog } from './services';
import { useSession } from './session';
import { useToasts } from './toasts';

export type ScopeSpec = { scope: 'library' } | { scope: 'playlist' | 'group'; id: string };

/** Songs of a scope, each once. */
export function scopeTracks(s: CatalogState, spec: ScopeSpec): Track[] {
  if (spec.scope === 'library') return libraryOrder(s);
  const ids = spec.scope === 'playlist' ? playlistTracks(s, spec.id).map((t) => t.id) : groupPlaylists(s, spec.id).flatMap((p) => playlistTracks(s, p.id).map((t) => t.id));
  return [...new Set(ids)].map((id) => s.tracks.get(id)).filter((t): t is Track => !!t);
}

export interface ScopeStats {
  total: number;
  synced: number;
  /** Uploading or waiting in a queue. */
  inQueue: number;
  /** Only on this computer and not queued yet. */
  toUpload: number;
  uploadBytes: number;
  attention: number;
}

export function scopeStats(s: CatalogState, spec: ScopeSpec): ScopeStats {
  const out: ScopeStats = { total: 0, synced: 0, inQueue: 0, toUpload: 0, uploadBytes: 0, attention: 0 };
  for (const tr of scopeTracks(s, spec)) {
    out.total++;
    const st = songStatus(tr, s.progress.get(tr.id));
    if (st.kind === 'local') {
      out.toUpload++;
      out.uploadBytes += tr.byte_size;
    } else if (st.kind === 'failed' || st.kind === 'relink' || st.kind === 'changed') out.attention++;
    else if (st.kind === 'synced' || st.kind === 'attention') out.synced++;
    else out.inQueue++;
  }
  return out;
}

export function scopePath(spec: ScopeSpec): string {
  return spec.scope === 'library' ? '/me/sync-scopes/library' : `/me/sync-scopes/${spec.scope}/${spec.id}`;
}

export function findScope(s: CatalogState, spec: ScopeSpec): Scope | undefined {
  const id = spec.scope === 'library' ? null : spec.id;
  return s.scopes.find((x) => x.type === spec.scope && (x.collection_id ?? null) === id);
}

/**
 * The scope's on/off state and the action that flips it. Turning it on applies
 * at once (no confirmation); the toast says how much will upload.
 */
export function useSyncToggle(spec: ScopeSpec, opts: { onDetails?: () => void } = {}) {
  const { t, locale } = useI18n();
  const s = useCatalog();
  const { me } = useSession();
  const toasts = useToasts();
  const [busy, setBusy] = useState(false);
  const enabled = findScope(s, spec)?.state === 'enabled';
  const cloud = !!me?.cloud_sync_enabled;

  const toggle = async () => {
    if (busy || !cloud) return;
    const turnOn = !enabled;
    const stats = scopeStats(catalog.getState(), spec);
    setBusy(true);
    try {
      const r = await api.mutate<{ scopes: Scope[] }>(turnOn ? 'PUT' : 'POST', scopePath(spec) + (turnOn ? '' : '/pause'));
      catalog.setScopes(r.scopes);
      if (!turnOn) toasts.show(t('scope.paused'), { icon: 'check' });
      else if (platform.kind !== 'web' && stats.toUpload > 0)
        toasts.show(t('sync.queuedSize', { n: stats.toUpload, size: formatBytes(locale, stats.uploadBytes) }), { icon: 'cloudUp', action: opts.onDetails ? { label: t('sync.details'), run: opts.onDetails } : undefined });
      else toasts.show(t('scope.enabled'), { icon: 'cloudCheck' });
    } catch (e) {
      toasts.error(e);
    } finally {
      setBusy(false);
    }
  };

  return { enabled, busy, cloud, toggle };
}
