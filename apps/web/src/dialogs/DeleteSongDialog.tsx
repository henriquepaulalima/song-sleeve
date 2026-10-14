import { useState } from 'react';
import { songStatus } from '@song-sleeve/core';
import { useCatalog } from '../app/hooks';
import { useT } from '../app/i18n';
import { api, catalog, markDeletedHere } from '../app/services';
import { useErrorText, useToasts } from '../app/toasts';
import { Alert, Dialog } from '../components/common';
import { Icon } from '../components/Icon';
import { StatusBadge, statusView } from '../components/SongRow';

/**
 * Delete song (§7.8, designs/WebDeleteSong). A synced song is removed from the
 * cloud, every device and all playlists; a syncing one has its sync failed. The
 * deleting device stops playback at once (R-PIN-5).
 */
export default function DeleteSongDialog({ trackId, onClose }: { trackId: string; onClose: () => void }) {
  const t = useT();
  const errorText = useErrorText();
  const toasts = useToasts();
  const cat = useCatalog();
  const track = cat.tracks.get(trackId);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (!track) return null;
  const syncing = track.state === 'pending_upload';
  const status = statusView(songStatus(track, cat.progress.get(track.id)), t);

  const confirm = async () => {
    setBusy(true);
    setError(null);
    try {
      const r = await api.mutate<{ deleted: boolean }>('DELETE', `/me/tracks/${track.id}?reason=delete`);
      if (r.deleted) {
        markDeletedHere(track.id);
        catalog.removeTrack(track.id);
      } else {
        toasts.show(t('delete.waiting'));
      }
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog role="alertdialog" onClose={onClose} hideClose width={540} labelledBy="del-song-t" describedBy="del-song-d">
      <span aria-hidden="true" className="danger-icon">
        <Icon name="trash" size={26} />
      </span>
      <div style={{ marginTop: 16 }}>
        <StatusBadge label={status.label} kind={status.kind} icon={status.icon} />
      </div>
      <h1 id="del-song-t" className="dialog-title" style={{ marginTop: 10, fontSize: 26, fontStretch: '112%', padding: 0 }}>
        {syncing ? t('delete.titleSyncing', { name: track.name }) : t('delete.titleSynced', { name: track.name })}
      </h1>
      <p id="del-song-d" style={{ margin: '12px 0 0', fontSize: 16, lineHeight: 1.5, color: 'var(--text-soft)' }}>
        {syncing ? t('delete.bodySyncing') : t('delete.bodySynced')}
      </p>
      <p style={{ margin: '10px 0 0', fontSize: 13, color: 'var(--muted-2)' }}>{syncing ? t('delete.footSyncing') : t('delete.footSynced')}</p>
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
          {t('menu.delete')}
        </button>
      </div>
    </Dialog>
  );
}
