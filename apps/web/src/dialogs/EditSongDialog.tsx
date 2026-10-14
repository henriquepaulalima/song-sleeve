import { useId, useState } from 'react';
import { MAX_SONG_NAME, cleanName, nameProblem, type Track } from '@song-sleeve/core';
import { useCatalog } from '../app/hooks';
import { useT } from '../app/i18n';
import { api, catalog } from '../app/services';
import { useErrorText } from '../app/toasts';
import { ArtistField, choiceToRef, type ArtistChoice } from '../components/ArtistField';
import { Alert, Dialog } from '../components/common';

/**
 * Edit song (§7.5): name and artist with the smart search. A synced song needs a
 * connection; the change applies only once the server accepts it (R-MUT-1, 2).
 * A replacement field in conflict comes back here as a suggestion (R-MUT-5).
 */
export default function EditSongDialog({ trackId, onClose }: { trackId: string; onClose: () => void }) {
  const t = useT();
  const errorText = useErrorText();
  const cat = useCatalog();
  const track = cat.tracks.get(trackId);
  const artist = track?.artist_id ? cat.artists.get(track.artist_id) : undefined;
  const [name, setName] = useState(track?.name ?? '');
  const [choice, setChoice] = useState<ArtistChoice>(artist ? { kind: 'existing', id: artist.id, name: artist.name } : { kind: 'none' });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const nameId = useId();
  if (!track) return null;
  const suggestion = track.metadata_suggestion;
  const problem = nameProblem(name, MAX_SONG_NAME);

  const save = async (patch?: { name?: string; artist?: ReturnType<typeof choiceToRef> }) => {
    setBusy(true);
    setError(null);
    try {
      const body: Record<string, unknown> = { expected_revision: track.revision, resolve_suggestion: !!suggestion };
      if (patch) Object.assign(body, patch);
      else {
        body.name = cleanName(name);
        body.artist = choiceToRef(choice);
      }
      const r = await api.mutate<{ track: Track }>('PATCH', `/me/tracks/${track.id}`, body);
      // A newly created artist arrives with the change feed.
      catalog.upsertTrack(r.track);
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t('edit.title')} onClose={onClose} width={560}>
      {suggestion && (
        <div role="status" style={{ marginTop: 16, padding: 14, borderRadius: 14, background: 'var(--sand)', color: 'var(--ink)', fontSize: 14, lineHeight: 1.45 }}>
          <strong>{t('edit.suggestionTitle')}</strong>
          {suggestion.name && <div style={{ marginTop: 6 }}>{t('edit.suggestionName', { value: suggestion.name })}</div>}
          {suggestion.artist && <div style={{ marginTop: 6 }}>{t('edit.suggestionArtist', { value: suggestion.artist.name ?? (suggestion.artist.id ? cat.artists.get(suggestion.artist.id)?.name ?? '' : '') })}</div>}
          <div style={{ marginTop: 10, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <button
              type="button"
              className="btn btn-sm"
              style={{ background: 'var(--ink)', color: 'var(--sand)' }}
              disabled={busy}
              onClick={() => void save({ ...(suggestion.name ? { name: suggestion.name } : {}), ...(suggestion.artist ? { artist: suggestion.artist } : {}) })}
            >
              {t('edit.useSuggestion')}
            </button>
            <button type="button" className="btn btn-sm" style={{ background: 'transparent', color: 'var(--ink)', boxShadow: 'inset 0 0 0 1.5px rgba(22,22,21,.5)' }} disabled={busy} onClick={() => void save({})}>
              {t('edit.keepCurrent')}
            </button>
          </div>
        </div>
      )}
      <div className="field" style={{ marginTop: 20, gap: 6 }}>
        <label className="label-sm" htmlFor={nameId}>
          {t('edit.name')}
        </label>
        <input className="input" id={nameId} type="text" value={name} aria-invalid={!!problem} onChange={(e) => setName(e.target.value)} data-autofocus />
      </div>
      <div className="field" style={{ marginTop: 16, gap: 6 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span className="label-sm">
            {t('edit.artist')} <span style={{ fontWeight: 400, color: 'var(--muted-2)' }}>{t('common.optional')}</span>
          </span>
          {choice.kind !== 'none' && (
            <button type="button" className="link-btn" style={{ fontSize: 13, fontWeight: 600, minHeight: 32 }} onClick={() => setChoice({ kind: 'none' })}>
              {t('edit.clearArtist')}
            </button>
          )}
        </div>
        <ArtistField key={choice.kind === 'none' ? 'none' : choice.name} label={t('edit.artist')} value={choice} onChange={setChoice} />
      </div>
      <p style={{ margin: '16px 0 0', fontSize: 13, lineHeight: 1.45, color: 'var(--muted-2)' }}>{t('edit.syncedNote', { max: MAX_SONG_NAME })}</p>
      {error && (
        <div style={{ marginTop: 12 }}>
          <Alert>{error}</Alert>
        </div>
      )}
      <div className="dialog-actions">
        <button type="button" className="btn btn-secondary btn-md" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="button" className="btn btn-primary btn-md" disabled={!!problem || busy} onClick={() => void save()}>
          {t('common.saveChanges')}
        </button>
      </div>
    </Dialog>
  );
}
