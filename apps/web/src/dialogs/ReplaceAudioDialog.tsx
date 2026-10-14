import { useId, useRef, useState } from 'react';
import { MAX_SONG_NAME, artistHintFromName, checkLimits, cleanName, didYouMean, formatBytes, formatClock, nameProblem, type LimitError, type MessageKey, type OperationDoc, type Track } from '@song-sleeve/core';
import { useCatalog } from '../app/hooks';
import { useI18n } from '../app/i18n';
import { api, catalog, replacements } from '../app/services';
import { useErrorText, useToasts } from '../app/toasts';
import { choiceToRef, textToChoice, useArtistCandidates, type ArtistChoice } from '../components/ArtistField';
import { Alert, Dialog } from '../components/common';
import { Icon } from '../components/Icon';
import { baseName, extractFacts } from '../upload/extract';

type Facts = { durationMs: number; format: 'mp3' | 'aac' | 'flac' | 'wav'; sha256: string; size: number };

/**
 * Replace audio file (§7.7): pick a file, then choose Name and Artist field by
 * field — Keep current, Use new, or edit — before anything is applied or uploaded
 * (R-MUT-5). Every confirmed replacement creates fresh content (R-ID-5); the old
 * audio stays current until the switch, and a failure keeps it (R-ID-4).
 */
export default function ReplaceAudioDialog({ trackId, onClose }: { trackId: string; onClose: () => void }) {
  const { t, locale } = useI18n();
  const errorText = useErrorText();
  const toasts = useToasts();
  const cat = useCatalog();
  const artists = useArtistCandidates();
  const track = cat.tracks.get(trackId);
  const curArtist = track?.artist_id ? cat.artists.get(track.artist_id) : undefined;
  const input = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [facts, setFacts] = useState<Facts | null>(null);
  const [reading, setReading] = useState(false);
  const [limit, setLimit] = useState<LimitError | null>(null);
  const [newName, setNewName] = useState('');
  const [newArtist, setNewArtist] = useState<string | null>(null);
  const [nameMode, setNameMode] = useState<'keep' | 'use'>('keep');
  const [artistMode, setArtistMode] = useState<'keep' | 'use'>('keep');
  const [nameValue, setNameValue] = useState(track?.name ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const nameId = useId();
  if (!track) return null;

  const pick = async (f: File) => {
    setFile(f);
    setFacts(null);
    setLimit(null);
    setError(null);
    setReading(true);
    const r = await extractFacts(f);
    setReading(false);
    const problem = checkLimits({ durationMs: r.durationMs, byteSize: f.size, format: r.format });
    if (problem || !r.sha256 || !r.durationMs || !r.format) {
      // An invalid file stops here and changes nothing (R-LIM-3).
      setLimit(problem ?? 'invalid_audio_metadata');
      return;
    }
    const extractedName = (r.title && cleanName(r.title)) || baseName(f);
    const hint = r.artist ?? artistHintFromName(extractedName);
    setNewName(extractedName);
    setNewArtist(hint);
    setNameMode(extractedName !== track.name ? 'use' : 'keep');
    setNameValue(extractedName !== track.name ? extractedName : track.name);
    // A new artist is only ever a suggestion (R-ORG-2): keep the current one by default.
    setArtistMode('keep');
    setFacts({ durationMs: r.durationMs, format: r.format, sha256: r.sha256, size: f.size });
  };

  const artistChoice: ArtistChoice =
    artistMode === 'keep' ? (curArtist ? { kind: 'existing', id: curArtist.id, name: curArtist.name } : { kind: 'none' }) : newArtist ? textToChoice(newArtist, artists) : { kind: 'none' };
  const similar = artistMode === 'use' && newArtist && artistChoice.kind === 'new' ? didYouMean(newArtist, artists) : null;
  const problem = nameProblem(nameValue, MAX_SONG_NAME);

  const confirm = async () => {
    if (!facts || !file || problem) return;
    setBusy(true);
    setError(null);
    try {
      const r = await api.mutate<{ track: Track; operation: OperationDoc }>('POST', `/me/tracks/${track.id}/replacement`, {
        expected_revision: track.revision,
        name: cleanName(nameValue),
        artist: choiceToRef(artistChoice),
        duration_ms: facts.durationMs,
        byte_size: facts.size,
        format: facts.format,
        sha256: facts.sha256,
      });
      catalog.upsertTrack(r.track);
      replacements.start(track.id, file, { durationMs: facts.durationMs, byteSize: facts.size, format: facts.format, sha256: facts.sha256 }, r.operation, (reason) =>
        toasts.show(t('sync.failedToast', { name: track.name, reason: t(`reason.${reason}` as MessageKey) }), { kind: 'alert' }),
      );
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const choice = (on: boolean, label: string, value: string, onClick: () => void) => (
    <button
      type="button"
      role="radio"
      aria-checked={on}
      onClick={onClick}
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'flex-start',
        gap: 2,
        minHeight: 58,
        padding: '8px 12px',
        borderRadius: 12,
        border: 0,
        color: 'var(--white)',
        font: 'inherit',
        textAlign: 'left',
        cursor: 'pointer',
        background: on ? 'var(--well)' : 'transparent',
        boxShadow: on ? 'inset 0 0 0 2px var(--sand)' : 'inset 0 0 0 1.5px rgba(255,255,255,.2)',
        minWidth: 0,
      }}
    >
      <span style={{ fontSize: 11, fontWeight: 700, color: 'var(--muted-2)', letterSpacing: '.04em' }}>{label}</span>
      <span className="ellipsis" style={{ fontSize: 15, maxWidth: '100%' }}>
        {value}
      </span>
    </button>
  );

  const detail = (label: string, oldV: string, newV: string, first?: boolean) => (
    <div style={{ display: 'grid', gridTemplateColumns: '90px 1fr 20px 1fr', alignItems: 'center', gap: 8, minHeight: 44, fontSize: 14, boxShadow: first ? 'none' : 'inset 0 1px 0 rgba(255,255,255,.07)' }}>
      <span style={{ color: 'var(--muted-2)' }}>{label}</span>
      <span className="mono" style={{ fontSize: 13, color: 'var(--muted-2)', textDecoration: 'line-through' }}>
        {oldV}
      </span>
      <span aria-hidden="true" style={{ color: 'var(--sand)' }}>
        →
      </span>
      <span className="mono" style={{ fontSize: 13, fontWeight: 600 }}>
        {newV}
      </span>
    </div>
  );

  return (
    <Dialog title={t('replace.title')} onClose={onClose} width={900}>
      <p style={{ margin: '6px 0 0', fontSize: 15, color: 'var(--muted)' }}>{t('replace.for', { name: track.name })}</p>
      <input
        ref={input}
        type="file"
        accept="audio/*,.mp3,.m4a,.aac,.flac,.wav"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) void pick(f);
          e.target.value = '';
        }}
      />
      {!file ? (
        <div style={{ marginTop: 20, padding: 18, borderRadius: 16, background: 'var(--well)', display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 14 }}>
          <span style={{ flex: '1 1 300px', fontSize: 15, color: 'var(--text-soft)' }}>{t('replace.pickPrompt')}</span>
          <button type="button" className="btn btn-primary" onClick={() => input.current?.click()} data-autofocus>
            <Icon name="file" size={18} />
            {t('replace.pick')}
          </button>
        </div>
      ) : (
        <div style={{ marginTop: 18, display: 'flex', alignItems: 'center', gap: 14, padding: '12px 14px', borderRadius: 14, background: 'var(--well)' }}>
          <span aria-hidden="true" style={{ width: 44, height: 44, borderRadius: 12, background: 'var(--sand)', color: 'var(--ink)', display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 'none' }}>
            <Icon name="file" />
          </span>
          <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
            <span className="mono ellipsis" style={{ fontSize: 14, fontWeight: 600 }}>
              {file.name}
            </span>
            <span style={{ fontSize: 13, color: 'var(--muted-2)' }}>{reading ? t('add.reading') : limit ? '' : t('replace.readable')}</span>
          </span>
          <button type="button" className="btn btn-sm" style={{ background: 'var(--charcoal)', color: 'var(--white)' }} onClick={() => input.current?.click()}>
            {t('replace.another')}
          </button>
        </div>
      )}
      {limit && (
        <div style={{ marginTop: 12 }}>
          <Alert>{t(`limit.${limit}` as MessageKey)}</Alert>
        </div>
      )}
      {facts && (
        <div style={{ marginTop: 20, display: 'flex', flexWrap: 'wrap', gap: 24 }}>
          <div style={{ flex: '1.3 1 380px', display: 'flex', flexDirection: 'column', gap: 16, minWidth: 0 }}>
            <div role="radiogroup" aria-label={t('replace.name')} style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              <span className="label-sm">{t('replace.name')}</span>
              <div style={{ display: 'flex', gap: 8 }}>
                {choice(nameMode === 'keep', t('replace.keepCurrent'), track.name, () => (setNameMode('keep'), setNameValue(track.name)))}
                {choice(nameMode === 'use', t('replace.useNew'), newName, () => (setNameMode('use'), setNameValue(newName)))}
              </div>
              <label htmlFor={nameId} className="sr-only">
                {t('replace.name')}
              </label>
              <input className="input compact" id={nameId} type="text" value={nameValue} aria-invalid={!!problem} onChange={(e) => setNameValue(e.target.value)} />
            </div>
            <div role="radiogroup" aria-label={t('replace.artist')} style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              <span className="label-sm">{t('replace.artist')}</span>
              <div style={{ display: 'flex', gap: 8 }}>
                {choice(artistMode === 'keep', t('replace.keepCurrent'), curArtist?.name ?? t('common.noArtist'), () => setArtistMode('keep'))}
                {choice(artistMode === 'use', t('replace.useNew'), newArtist ?? t('common.noArtist'), () => setArtistMode('use'))}
              </div>
              {similar && (
                <p role="status" style={{ margin: 0, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, padding: '8px 12px', borderRadius: 10, background: 'var(--sand)', color: 'var(--ink)', fontSize: 13 }}>
                  {t('edit.didYouMean', { name: similar.name })}
                  <button type="button" className="btn btn-sm" style={{ minHeight: 30, background: 'var(--ink)', color: 'var(--sand)' }} onClick={() => setNewArtist(similar.name)}>
                    {t('edit.useIt')}
                  </button>
                </p>
              )}
            </div>
          </div>
          <div style={{ flex: '1 1 280px' }}>
            <h2 style={{ margin: '0 0 6px', fontSize: 13, fontWeight: 600, letterSpacing: '.06em', color: 'var(--muted)' }}>{t('replace.details')}</h2>
            <div style={{ padding: '4px 14px', borderRadius: 14, background: 'var(--well)' }}>
              {detail(t('replace.duration'), formatClock(track.duration_ms / 1000), formatClock(facts.durationMs / 1000), true)}
              {detail(t('replace.size'), formatBytes(locale, track.byte_size), formatBytes(locale, facts.size))}
              {detail(t('replace.format'), track.format.toUpperCase(), facts.format.toUpperCase())}
            </div>
            <p style={{ margin: '12px 0 0', fontSize: 13, lineHeight: 1.5, color: 'var(--text-soft)' }}>{t('replace.after')}</p>
          </div>
        </div>
      )}
      {error && (
        <div style={{ marginTop: 12 }}>
          <Alert>{error}</Alert>
        </div>
      )}
      <div className="dialog-actions" style={{ marginTop: 22 }}>
        <button type="button" className="btn btn-secondary btn-md" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="button" className="btn btn-primary btn-md" disabled={!facts || !!problem || busy} onClick={() => void confirm()} style={{ padding: '0 26px' }}>
          {t('replace.confirm')}
        </button>
      </div>
    </Dialog>
  );
}
