import { useId, useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router';
import { MAX_ARTIST_NAME, MAX_GROUP_NAME, MAX_PLAYLIST_NAME, charCount, cleanName, nameProblem, type Artist, type Group, type Playlist } from '@song-sleeve/core';
import type { DialogSpec } from '../app/dialogs';
import { useCatalog } from '../app/hooks';
import { useT } from '../app/i18n';
import { api, catalog } from '../app/services';
import { useErrorText, useToasts } from '../app/toasts';
import { Alert, Dialog } from '../components/common';

type Spec = Extract<DialogSpec, { kind: 'createPlaylist' | 'createGroup' | 'renamePlaylist' | 'renameGroup' | 'renameArtist' }>;

/** Create / rename playlist, group or artist (designs/WebCollectionDialogs). Online only. */
export default function NameDialog({ spec, onClose }: { spec: Spec; onClose: () => void }) {
  const t = useT();
  const navigate = useNavigate();
  const errorText = useErrorText();
  const toasts = useToasts();
  const cat = useCatalog();
  const id = useId();
  const current =
    spec.kind === 'renamePlaylist' ? cat.playlists.get(spec.id) : spec.kind === 'renameGroup' ? cat.groups.get(spec.id) : spec.kind === 'renameArtist' ? cat.artists.get(spec.id) : undefined;
  const [name, setName] = useState(current?.name ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const max = spec.kind === 'renameArtist' ? MAX_ARTIST_NAME : spec.kind.includes('Group') ? MAX_GROUP_NAME : MAX_PLAYLIST_NAME;
  const title = {
    createPlaylist: t('coll.createPlaylist'),
    createGroup: t('coll.createGroup'),
    renamePlaylist: t('coll.renamePlaylist'),
    renameGroup: t('coll.renameGroup'),
    renameArtist: t('coll.renameArtist'),
  }[spec.kind];
  const placeholder = spec.kind === 'createPlaylist' ? t('coll.newPlaylistPh') : spec.kind === 'createGroup' ? t('coll.newGroupPh') : '';
  const problem = nameProblem(name, max);
  const unchanged = current && cleanName(name) === current.name;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (problem || busy || unchanged) return;
    setBusy(true);
    setError(null);
    const value = cleanName(name);
    try {
      switch (spec.kind) {
        case 'createPlaylist': {
          const r = await api.mutate<{ playlist: Playlist }>('POST', '/me/playlists', { name: value });
          catalog.upsertPlaylist(r.playlist);
          if (spec.addTrackId) {
            // Created from a song's "Add to playlist": add it and stay on this page.
            const added = await api.mutate<{ playlist: Playlist }>('POST', `/me/playlists/${r.playlist.id}/songs`, { track_ids: [spec.addTrackId], expected_revision: r.playlist.revision });
            catalog.upsertPlaylist(added.playlist);
            onClose();
            toasts.show(t('toast.addedTo', { name: r.playlist.name }), { icon: 'check' });
            return;
          }
          onClose();
          navigate(`/playlists/${r.playlist.id}`);
          return;
        }
        case 'createGroup': {
          const r = await api.mutate<{ group: Group }>('POST', '/me/groups', { name: value });
          catalog.upsertGroup(r.group);
          onClose();
          navigate(`/groups/${r.group.id}`);
          return;
        }
        case 'renamePlaylist': {
          const r = await api.mutate<{ playlist: Playlist }>('PATCH', `/me/playlists/${spec.id}`, { name: value, expected_revision: current!.revision });
          catalog.upsertPlaylist(r.playlist);
          break;
        }
        case 'renameGroup': {
          const r = await api.mutate<{ group: Group }>('PATCH', `/me/groups/${spec.id}`, { name: value, expected_revision: current!.revision });
          catalog.upsertGroup(r.group);
          break;
        }
        case 'renameArtist': {
          const r = await api.mutate<{ artist: Artist }>('PATCH', `/me/artists/${spec.id}`, { name: value, expected_revision: current!.revision });
          catalog.upsertArtist(r.artist);
          break;
        }
      }
      onClose();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={title} onClose={onClose} width={540}>
      <form onSubmit={submit}>
        <div className="field" style={{ marginTop: 20 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <label htmlFor={id} className="label-sm">
              {t('coll.name')}
            </label>
            <span className="mono" style={{ fontSize: 12, color: charCount(cleanName(name)) > max ? 'var(--sand)' : 'var(--muted-2)' }}>
              {charCount(cleanName(name))} / {max}
            </span>
          </div>
          <input className="input" id={id} type="text" value={name} placeholder={placeholder} aria-invalid={problem === 'too_long'} onChange={(e) => setName(e.target.value)} data-autofocus />
        </div>
        <p style={{ margin: '12px 0 0', fontSize: 13, color: 'var(--muted-2)' }}>{t('coll.needsConnection')}</p>
        {error && (
          <div style={{ marginTop: 12 }}>
            <Alert>{error}</Alert>
          </div>
        )}
        <div className="dialog-actions" style={{ marginTop: 26 }}>
          <button type="button" className="btn btn-secondary btn-md" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="submit" className="btn btn-primary btn-md" disabled={!!problem || busy || !!unchanged}>
            {spec.kind.startsWith('create') ? t('common.create') : t('common.save')}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
