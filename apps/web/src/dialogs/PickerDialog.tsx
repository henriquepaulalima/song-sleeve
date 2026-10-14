import { useMemo, useState } from 'react';
import { groupPlaylists, groupsOfPlaylist, libraryOrder, matchesQuery, playlistTracks, type Group, type Playlist } from '@song-sleeve/core';
import type { DialogSpec } from '../app/dialogs';
import { useCatalog } from '../app/hooks';
import { useT } from '../app/i18n';
import { api, catalog } from '../app/services';
import { useErrorText } from '../app/toasts';
import { Alert, Dialog, SearchField } from '../components/common';
import { Icon, type IconName } from '../components/Icon';

type Spec = Extract<DialogSpec, { kind: 'addSongsToPlaylist' | 'addPlaylistsToGroup' | 'addToGroups' }>;

interface Option {
  id: string;
  name: string;
  meta: string;
  already: boolean;
}

/** Searchable multi-select pickers with Add (§8.6). */
export default function PickerDialog({ spec, onClose }: { spec: Spec; onClose: () => void }) {
  const t = useT();
  const errorText = useErrorText();
  const s = useCatalog();
  const [q, setQ] = useState('');
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { title, sub, alreadyLabel, icon, options } = useMemo(() => {
    if (spec.kind === 'addSongsToPlaylist') {
      const p = s.playlists.get(spec.playlistId);
      const inIt = new Set(p?.items.map((i) => i.track_id));
      return {
        title: t('coll.addSongsTitle'),
        sub: p?.name ?? '',
        alreadyLabel: t('coll.inPlaylist'),
        icon: 'note' as IconName,
        options: libraryOrder(s).map<Option>((tr) => ({ id: tr.id, name: tr.name, meta: tr.artist_id ? (s.artists.get(tr.artist_id)?.name ?? '') : t('common.noArtist'), already: inIt.has(tr.id) })),
      };
    }
    if (spec.kind === 'addPlaylistsToGroup') {
      const g = s.groups.get(spec.groupId);
      const inIt = new Set(g?.playlists.map((m) => m.playlist_id));
      return {
        title: t('coll.addPlaylistsTitle'),
        sub: g?.name ?? '',
        alreadyLabel: t('coll.inGroup'),
        icon: 'songs' as IconName,
        options: [...s.playlists.values()].sort((a, b) => a.name.localeCompare(b.name)).map<Option>((p) => ({ id: p.id, name: p.name, meta: t('count.songs', { n: playlistTracks(s, p.id).length }), already: inIt.has(p.id) })),
      };
    }
    const p = s.playlists.get(spec.playlistId);
    const inGroups = new Set(groupsOfPlaylist(s, spec.playlistId).map((g) => g.id));
    return {
      title: t('coll.addToGroupsTitle'),
      sub: t('coll.addToGroupsSub', { name: p?.name ?? '' }),
      alreadyLabel: t('coll.alreadyIn'),
      icon: 'group' as IconName,
      options: [...s.groups.values()].sort((a, b) => a.name.localeCompare(b.name)).map<Option>((g) => ({ id: g.id, name: g.name, meta: t('count.playlists', { n: groupPlaylists(s, g.id).length }), already: inGroups.has(g.id) })),
    };
  }, [spec, s, t]);

  const shown = options.filter((o) => matchesQuery(q, o.name, o.meta)).slice(0, 300);
  const n = picked.size;

  const add = async () => {
    if (!n || busy) return;
    setBusy(true);
    setError(null);
    try {
      const ids = [...picked];
      if (spec.kind === 'addSongsToPlaylist') {
        const p = catalog.getState().playlists.get(spec.playlistId)!;
        const r = await api.mutate<{ playlist: Playlist }>('POST', `/me/playlists/${p.id}/songs`, { track_ids: ids, expected_revision: p.revision });
        catalog.upsertPlaylist(r.playlist);
      } else if (spec.kind === 'addPlaylistsToGroup') {
        const g = catalog.getState().groups.get(spec.groupId)!;
        const r = await api.mutate<{ group: Group }>('POST', `/me/groups/${g.id}/playlists`, { playlist_ids: ids, expected_revision: g.revision });
        catalog.upsertGroup(r.group);
      } else {
        // One accepted change per group; each needs its own revision.
        for (const gid of ids) {
          const g = catalog.getState().groups.get(gid)!;
          const r = await api.mutate<{ group: Group }>('POST', `/me/groups/${g.id}/playlists`, { playlist_ids: [spec.playlistId], expected_revision: g.revision });
          catalog.upsertGroup(r.group);
        }
      }
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={title} onClose={onClose} width={580}>
      <p style={{ margin: '6px 0 0', fontSize: 14, color: 'var(--muted)' }}>{sub}</p>
      <div style={{ marginTop: 16 }}>
        <SearchField label={t('common.search')} placeholder={t('common.search')} value={q} onChange={setQ} maxWidth={9999} />
      </div>
      <ul style={{ listStyle: 'none', margin: '10px 0 0', padding: 0, display: 'flex', flexDirection: 'column', gap: 2, maxHeight: 330, overflow: 'auto' }}>
        {shown.map((o) => {
          const on = picked.has(o.id);
          return (
            <li key={o.id}>
              <button
                type="button"
                className="opt"
                role="checkbox"
                aria-checked={o.already || on}
                disabled={o.already}
                onClick={() =>
                  setPicked((p) => {
                    const next = new Set(p);
                    if (next.has(o.id)) next.delete(o.id);
                    else next.add(o.id);
                    return next;
                  })
                }
              >
                <span aria-hidden="true" style={{ width: 40, height: 40, borderRadius: 10, background: 'var(--well)', color: 'var(--sand)', display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 'none' }}>
                  <Icon name={icon} size={18} />
                </span>
                <span style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}>
                  <span className="ellipsis" style={{ color: o.already ? 'var(--muted-3)' : 'var(--white)' }}>
                    {o.name}
                  </span>
                  <span className="ellipsis" style={{ fontSize: 12, color: 'var(--muted-2)' }}>
                    {o.meta}
                  </span>
                </span>
                {o.already ? (
                  <span style={{ fontSize: 12, color: 'var(--muted-2)' }}>{alreadyLabel}</span>
                ) : (
                  <span aria-hidden="true" className={'checkbox-box' + (on ? ' on' : '')} style={{ width: 26, height: 26, borderRadius: 8 }}>
                    {on && <Icon name="check" size={16} strokeWidth={3} />}
                  </span>
                )}
              </button>
            </li>
          );
        })}
        {shown.length === 0 && <li style={{ padding: 16, color: 'var(--muted-2)', fontSize: 14 }}>{q ? t('state.noResults', { query: q }) : t('menu.noPlaylists')}</li>}
      </ul>
      {error && (
        <div style={{ marginTop: 12 }}>
          <Alert>{error}</Alert>
        </div>
      )}
      <div className="dialog-actions" style={{ marginTop: 20 }}>
        <button type="button" className="btn btn-secondary btn-md" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="button" className="btn btn-primary btn-md" disabled={!n || busy} onClick={() => void add()}>
          {n ? t('common.addN', { n }) : t('common.add')}
        </button>
      </div>
    </Dialog>
  );
}
