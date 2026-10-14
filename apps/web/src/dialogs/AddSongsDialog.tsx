import { useEffect, useId, useRef, useState } from 'react';
import { artistHintFromName, checkLimits, cleanName, MAX_SONG_NAME, nameProblem, newestFirst, type LimitError, type MessageKey } from '@song-sleeve/core';
import { useCatalog, useSignal } from '../app/hooks';
import { useI18n } from '../app/i18n';
import { online, uploads } from '../app/services';
import { useToasts } from '../app/toasts';
import { ArtistField, choiceToRef, type ArtistChoice } from '../components/ArtistField';
import { Dialog } from '../components/common';
import { Icon } from '../components/Icon';
import { DownloadButtons } from '../screens/Misc';
import { baseName, extractFacts, looksLikeAudio } from '../upload/extract';

interface Row {
  key: string;
  file: File;
  name: string;
  artist: ArtistChoice;
  hint: string | null;
  reading: boolean;
  facts: { durationMs: number; format: 'mp3' | 'aac' | 'flac' | 'wav'; sha256: string } | null;
  problem: LimitError | null;
}

/**
 * Add songs on the web (§7.2): every song is uploaded to the cloud library and
 * runs through the tab's sync queue, three at a time. "Keep only on this device"
 * shows the install-app alert and imports nothing (R-CLOUD-2).
 */
export default function AddSongsDialog({ playlistId, onClose }: { playlistId?: string; onClose: () => void }) {
  const { t } = useI18n();
  const toasts = useToasts();
  const isOnline = useSignal(online);
  const cat = useCatalog();
  const [rows, setRows] = useState<Row[]>([]);
  const [skipped, setSkipped] = useState<string[]>([]);
  const [playlist, setPlaylist] = useState(playlistId ?? '');
  const [installAlert, setInstallAlert] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const plId = useId();
  const playlists = newestFirst(cat.playlists.values());

  const addFiles = (files: File[]) => {
    const audio = files.filter(looksLikeAudio);
    const rejected = files.filter((f) => !looksLikeAudio(f)).map((f) => f.name);
    if (rejected.length) setSkipped((s) => [...s, ...rejected]);
    const fresh: Row[] = audio.map((file) => {
      const name = baseName(file);
      return { key: crypto.randomUUID(), file, name, artist: { kind: 'none' }, hint: artistHintFromName(name), reading: true, facts: null, problem: null };
    });
    setRows((r) => [...r, ...fresh]);
    for (const row of fresh) {
      void extractFacts(row.file).then((f) => {
        setRows((rs) =>
          rs.map((r) => {
            if (r.key !== row.key) return r;
            const problem = checkLimits({ durationMs: f.durationMs, byteSize: row.file.size, format: f.format });
            if (!f.format) {
              setSkipped((s) => (s.includes(row.file.name) ? s : [...s, row.file.name]));
              return { ...r, reading: false, problem: 'unsupported_format' as const };
            }
            const title = f.title ? cleanName(f.title) : null;
            const name = title || r.name;
            // A suggestion never assigns an artist on its own (R-ORG-2).
            const hint = f.artist ?? artistHintFromName(name) ?? r.hint;
            return {
              ...r,
              name,
              hint,
              reading: false,
              problem: problem ?? (f.sha256 ? null : 'invalid_audio_metadata'),
              facts: f.sha256 && f.durationMs ? { durationMs: f.durationMs, format: f.format, sha256: f.sha256 } : null,
            };
          }),
        );
      });
    }
  };

  // Unsupported files are reported and dropped from the list.
  const visible = rows.filter((r) => r.problem !== 'unsupported_format');
  const flagged = visible.filter((r) => r.problem || nameProblem(r.name, MAX_SONG_NAME));
  const reading = visible.some((r) => r.reading);
  const ready = visible.length > 0 && flagged.length === 0 && !reading;

  useEffect(() => {
    if (rows.some((r) => r.problem === 'unsupported_format')) setRows((rs) => rs.filter((r) => r.problem !== 'unsupported_format'));
  }, [rows]);

  const confirm = () => {
    if (!ready) return;
    if (!isOnline) {
      toasts.show(t('state.connectionRequired'));
      return;
    }
    uploads.enqueue(
      visible.map((r) => ({
        file: r.file,
        name: cleanName(r.name),
        artist: choiceToRef(r.artist),
        playlistIds: playlist ? [playlist] : [],
        facts: { durationMs: r.facts!.durationMs, byteSize: r.file.size, format: r.facts!.format, sha256: r.facts!.sha256 },
      })),
    );
    onClose();
  };

  const update = (key: string, patch: Partial<Row>) => setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
  const limitText = (p: LimitError) => t(`limit.${p}` as MessageKey);

  return (
    <>
      <Dialog title={t('add.title')} onClose={onClose} width={940}>
        <div
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={(e) => {
            e.preventDefault();
            setDragOver(false);
            addFiles([...e.dataTransfer.files]);
          }}
          style={dragOver ? { outline: '2px dashed var(--sand)', outlineOffset: 8, borderRadius: 16 } : undefined}
        >
          <div role="note" style={{ marginTop: 14, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, padding: '12px 14px', borderRadius: 14, background: 'var(--well)' }}>
            <Icon name="cloudUp" size={20} style={{ color: 'var(--sand)' }} />
            <span style={{ flex: '1 1 300px', fontSize: 14 }}>{t('add.webBanner')}</span>
            <button type="button" className="btn btn-secondary btn-sm" onClick={() => setInstallAlert(true)}>
              {t('add.keepLocal')}
            </button>
          </div>

          <div style={{ marginTop: 16, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
            <button type="button" className="btn btn-well btn-sm" style={{ minHeight: 46, borderRadius: 14 }} onClick={() => fileInput.current?.click()}>
              <Icon name="file" size={18} />
              {t('add.files')}
            </button>
            <input
              ref={fileInput}
              type="file"
              multiple
              accept="audio/*,.mp3,.m4a,.aac,.flac,.wav"
              hidden
              onChange={(e) => {
                addFiles([...(e.target.files ?? [])]);
                e.target.value = '';
              }}
            />
            <span style={{ fontSize: 13, color: 'var(--muted-2)' }}>{t('add.hint')}</span>
          </div>

          {skipped.length > 0 && (
            <p role="status" className="note" style={{ marginTop: 12 }}>
              <Icon name="info" />
              <span>
                <strong style={{ color: 'var(--white)' }}>{t('add.skipped', { n: skipped.length })}</strong> {t('add.skippedReason', { files: skipped.slice(0, 4).join(', ') + (skipped.length > 4 ? '…' : '') })}
              </span>
            </p>
          )}

          {visible.length === 0 ? (
            <p style={{ margin: '24px 0 8px', padding: 24, borderRadius: 16, textAlign: 'center', color: 'var(--muted-2)', boxShadow: 'inset 0 0 0 1.5px rgba(255,255,255,.12)' }}>{t('add.empty')}</p>
          ) : (
            <>
              <div style={{ marginTop: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <h2 style={{ margin: 0, fontSize: 16, fontWeight: 700 }}>{flagged.length ? t('add.toReview', { n: visible.length }) : t('add.toAdd', { n: visible.length })}</h2>
                <button type="button" className="link-btn" style={{ fontSize: 13, fontWeight: 600 }} onClick={() => setRows([])}>
                  {t('add.removeAll')}
                </button>
              </div>
              <div style={{ marginTop: 8, maxHeight: '42vh', overflowY: 'auto', paddingBottom: 4 }}>
                <div aria-hidden="true" style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1.1fr) minmax(0,1.3fr) minmax(0,1.4fr) 40px', columnGap: 12, paddingBottom: 6 }}>
                  <span className="th">{t('add.colFile')}</span>
                  <span className="th">{t('add.colName')}</span>
                  <span className="th">{t('add.colArtist')}</span>
                  <span />
                </div>
                {visible.map((r) => {
                  const nameErr = nameProblem(r.name, MAX_SONG_NAME);
                  return (
                    <div
                      key={r.key}
                      style={{
                        display: 'grid',
                        gridTemplateColumns: 'minmax(0,1.1fr) minmax(0,1.3fr) minmax(0,1.4fr) 40px',
                        columnGap: 12,
                        alignItems: 'start',
                        padding: '10px 0',
                        paddingLeft: r.problem ? 10 : 0,
                        boxShadow: r.problem ? 'inset 0 1px 0 rgba(255,255,255,.08), inset 3px 0 0 var(--sand)' : 'inset 0 1px 0 rgba(255,255,255,.08)',
                      }}
                    >
                      <span className="mono ellipsis" style={{ paddingTop: 12, fontSize: 12, color: 'var(--muted-2)' }} title={r.file.name}>
                        {r.file.name}
                      </span>
                      {r.problem ? (
                        <span role="alert" style={{ gridColumn: 'span 2', display: 'flex', gap: 8, alignItems: 'center', minHeight: 44, padding: '0 12px', borderRadius: 12, background: 'var(--sand)', color: 'var(--ink)', fontSize: 13, fontWeight: 600 }}>
                          <Icon name="alert" size={16} strokeWidth={2.4} />
                          {limitText(r.problem)}
                        </span>
                      ) : (
                        <>
                          <span style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                            <input
                              className="input compact"
                              type="text"
                              aria-label={t('add.colName')}
                              value={r.name}
                              aria-invalid={!!nameErr}
                              disabled={r.reading}
                              onChange={(e) => update(r.key, { name: e.target.value })}
                            />
                            {r.reading && <span style={{ fontSize: 12, color: 'var(--muted-2)' }}>{t('add.reading')}</span>}
                          </span>
                          <ArtistField compact label={t('add.colArtist')} value={r.artist} hint={r.hint} onChange={(c) => update(r.key, { artist: c })} />
                        </>
                      )}
                      <button type="button" className="icon-btn" style={{ width: 40, height: 40, background: 'var(--well)', marginTop: 2 }} aria-label={t('add.remove', { name: r.name })} onClick={() => setRows((rs) => rs.filter((x) => x.key !== r.key))}>
                        <Icon name="close" size={16} />
                      </button>
                    </div>
                  );
                })}
              </div>
            </>
          )}

          <div style={{ marginTop: 16, display: 'flex', flexWrap: 'wrap', gap: 12 }}>
            <div style={{ flex: '1 1 260px' }}>
              <label htmlFor={plId} className="label-sm" style={{ display: 'block', marginBottom: 6 }}>
                {t('add.playlist')}
              </label>
              <div className="select-wrap">
                <select id={plId} className="input compact" value={playlist} onChange={(e) => setPlaylist(e.target.value)}>
                  <option value="">{t('add.none')}</option>
                  {playlists.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
                <Icon name="chevronDown" />
              </div>
            </div>
          </div>

          <div className="dialog-actions" style={{ marginTop: 18, alignItems: 'center' }}>
            {flagged.some((r) => r.problem) && <span style={{ marginRight: 'auto', fontSize: 13, color: 'var(--sand)' }}>{t('add.fixFlagged')}</span>}
            <button type="button" className="btn btn-secondary btn-md" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button type="button" className="btn btn-primary btn-md" disabled={!ready} onClick={confirm} style={{ padding: '0 26px' }}>
              {t('add.upload', { n: visible.length })}
            </button>
          </div>
        </div>
      </Dialog>
      {installAlert && (
        <Dialog role="alertdialog" onClose={() => setInstallAlert(false)} hideClose width={520} labelledBy="install-t">
          <h2 id="install-t" style={{ margin: 0, fontSize: 22, lineHeight: 1.3, fontWeight: 700 }}>
            {t('add.installTitle')}
          </h2>
          <div style={{ marginTop: 18 }}>
            <DownloadButtons compact />
          </div>
          <button type="button" className="btn btn-secondary btn-block" style={{ marginTop: 14 }} onClick={() => setInstallAlert(false)}>
            {t('common.ok')}
          </button>
        </Dialog>
      )}
    </>
  );
}

