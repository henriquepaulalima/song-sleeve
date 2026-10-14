import { useState } from 'react';
import { useNavigate } from 'react-router';
import { groupPlaylists, groupsOfPlaylist, playlistTracks } from '@song-sleeve/core';
import type { DialogSpec } from '../app/dialogs';
import { useCatalog } from '../app/hooks';
import { useT } from '../app/i18n';
import { api, catalog } from '../app/services';
import { useErrorText } from '../app/toasts';
import { Alert, Dialog } from '../components/common';
import { Icon } from '../components/Icon';

type Spec = Extract<DialogSpec, { kind: 'deletePlaylist' | 'deleteGroup' | 'deleteArtist' }>;

/** Delete playlist / group / artist with what stays and what goes (R-ORG-5). */
export default function DeleteCollectionDialog({ spec, onClose }: { spec: Spec; onClose: () => void }) {
  const t = useT();
  const navigate = useNavigate();
  const errorText = useErrorText();
  const s = useCatalog();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  let title = '';
  let action = '';
  let lines: { text: string; keep: boolean }[] = [];
  if (spec.kind === 'deletePlaylist') {
    const p = s.playlists.get(spec.id);
    if (!p) return null;
    const groups = groupsOfPlaylist(s, p.id);
    const n = playlistTracks(s, p.id).length;
    const fromGroup = spec.fromGroupId ? s.groups.get(spec.fromGroupId) : undefined;
    title = t('coll.deletePlaylistTitle', { name: p.name });
    action = t('coll.deletePlaylist');
    lines = fromGroup
      ? [
          { text: t('coll.deletePlaylistFromGroupGone', { group: fromGroup.name }), keep: false },
          { text: t('coll.deletePlaylistFromGroupKeep', { group: fromGroup.name }), keep: true },
        ]
      : [
          { text: t('coll.deletePlaylistGone') + (groups.length ? ' ' + t('coll.deletePlaylistGroups', { groups: groups.map((g) => g.name).join(', ') }) : ''), keep: false },
          { text: t('coll.deletePlaylistKeep', { n }), keep: true },
        ];
  } else if (spec.kind === 'deleteGroup') {
    const g = s.groups.get(spec.id);
    if (!g) return null;
    title = t('coll.deleteGroupTitle', { name: g.name });
    action = t('coll.deleteGroup');
    lines = [
      { text: t('coll.deleteGroupGone'), keep: false },
      { text: t('coll.deleteGroupKeep', { n: groupPlaylists(s, g.id).length }), keep: true },
    ];
  } else {
    const a = s.artists.get(spec.id);
    if (!a) return null;
    title = t('coll.deleteArtistTitle', { name: a.name });
    action = t('coll.deleteArtist');
    lines = [
      { text: t('coll.deleteArtistGone'), keep: false },
      { text: t('coll.deleteArtistKeep'), keep: true },
    ];
  }

  const confirm = async () => {
    setBusy(true);
    setError(null);
    try {
      if (spec.kind === 'deletePlaylist') {
        await api.mutate('DELETE', `/me/playlists/${spec.id}`);
        catalog.removePlaylist(spec.id);
        if (!spec.fromGroupId) navigate('/library');
      } else if (spec.kind === 'deleteGroup') {
        await api.mutate('DELETE', `/me/groups/${spec.id}`);
        catalog.removeGroup(spec.id);
        navigate('/library');
      } else {
        await api.mutate('DELETE', `/me/artists/${spec.id}`);
        catalog.removeArtist(spec.id);
        navigate('/artists');
      }
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog role="alertdialog" onClose={onClose} hideClose width={540} labelledBy="del-coll-t">
      <span aria-hidden="true" className="danger-icon">
        <Icon name="trash" size={26} />
      </span>
      <h1 id="del-coll-t" className="dialog-title" style={{ marginTop: 16, fontSize: 26, fontStretch: '112%', padding: 0 }}>
        {title}
      </h1>
      <ul style={{ listStyle: 'none', margin: '16px 0 0', padding: 0, display: 'flex', flexDirection: 'column', gap: 10 }}>
        {lines.map((l) => (
          <li key={l.text} style={{ display: 'flex', alignItems: 'flex-start', gap: 10, fontSize: 15, lineHeight: 1.45, color: 'var(--text-soft)' }}>
            <Icon name={l.keep ? 'check' : 'close'} size={16} style={{ marginTop: 3, color: l.keep ? 'var(--sand)' : 'var(--orange)' }} />
            <span>{l.text}</span>
          </li>
        ))}
      </ul>
      {error && (
        <div style={{ marginTop: 12 }}>
          <Alert>{error}</Alert>
        </div>
      )}
      <div className="dialog-actions" style={{ marginTop: 26 }}>
        <button type="button" className="btn btn-secondary btn-md" onClick={onClose} data-autofocus>
          {t('common.cancel')}
        </button>
        <button type="button" className="btn btn-primary btn-md" disabled={busy} onClick={() => void confirm()}>
          {action}
        </button>
      </div>
    </Dialog>
  );
}
