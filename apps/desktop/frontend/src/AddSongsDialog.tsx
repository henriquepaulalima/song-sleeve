/**
 * Add songs on the desktop (§7.2, designs/WebAddSongs desktop variant): pick files
 * or a folder, review names and artists, and add them as local songs. Nothing is
 * copied and no connection is needed (R-LOCAL-1). "Sync to cloud now" queues them
 * for upload, three at a time. Replaces the web dialog in this build.
 */
import { useId, useState } from 'react';
import { cleanName, MAX_SONG_NAME, nameProblem, type MessageKey } from '@song-sleeve/core';
import { useCatalog } from '@web/app/hooks';
import { useI18n } from '@web/app/i18n';
import { useSession } from '@web/app/session';
import { useToasts } from '@web/app/toasts';
import { ArtistField, choiceName, type ArtistChoice } from '@web/components/ArtistField';
import { Dialog } from '@web/components/common';
import { Icon } from '@web/components/Icon';
import { PlaylistPicker } from '@web/components/PlaylistPicker';
import { local } from './bridge';
import { catchUp } from './services';

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

interface Row {
  key: string;
  c: Candidate;
  name: string;
  artist: ArtistChoice;
}

const fileName = (p: string) => p.split(/[\\/]/).pop() ?? p;

export default function AddSongsDialog({ playlistIds, artistId, onClose }: { playlistIds?: string[]; artistId?: string; onClose: () => void }) {
  const { t } = useI18n();
  const toasts = useToasts();
  const { me } = useSession();
  const cat = useCatalog();
  const [rows, setRows] = useState<Row[]>([]);
  const [skipped, setSkipped] = useState(0);
  const [reading, setReading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [playlists, setPlaylists] = useState<string[]>(playlistIds ?? []);
  const preset = artistId ? cat.artists.get(artistId) : undefined;
  const cloud = !!me?.cloud_sync_enabled;
  const autoLibrary = cat.scopes.some((s) => s.type === 'library' && s.state === 'enabled');
  const [sync, setSync] = useState(cloud && autoLibrary);
  const plId = useId();
  const syncId = useId();

  const pick = async (folder: boolean) => {
    try {
      const { paths } = await local<{ paths: string[] | null }>('POST', '/pick', { folder, title: folder ? t('add.folder') : t('add.files') });
      if (!paths?.length) return;
      setReading(true);
      const r = await local<{ candidates: Candidate[]; skipped: number }>('POST', '/analyze', { paths });
      setSkipped((n) => n + r.skipped + r.candidates.filter((c) => c.problem === 'unsupported_format').length);
      const known = new Set(rows.map((x) => x.c.path));
      const fresh = r.candidates
        .filter((c) => c.problem !== 'unsupported_format' && !known.has(c.path))
        // Opened from an artist page, files without an artist tag get that artist.
        .map((c): Row => ({ key: c.path, c, name: c.name, artist: preset && !c.artist_hint ? { kind: 'existing', id: preset.id, name: preset.name } : { kind: 'none' } }));
      setRows((rs) => [...rs, ...fresh]);
    } catch (e) {
      toasts.error(e);
    } finally {
      setReading(false);
    }
  };

  const flagged = rows.filter((r) => r.c.problem || nameProblem(r.name, MAX_SONG_NAME));
  const ready = rows.length > 0 && flagged.length === 0 && !reading && !busy;
  const update = (key: string, patch: Partial<Row>) => setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
  const problemText = (p: string) => (p === 'in_song_sleeve_folder' ? t('add.inSleeve') : p === 'unreadable' ? t('add.unreadable') : t(`limit.${p}` as MessageKey));

  const confirm = async () => {
    if (!ready) return;
    setBusy(true);
    try {
      await local('POST', '/import', {
        items: rows.map((r) => ({ ...r.c, name: cleanName(r.name), artist: choiceName(r.artist) })),
        playlist_ids: playlists,
        sync: sync && cloud,
      });
      await catchUp();
      onClose();
    } catch (e) {
      toasts.error(e);
    } finally {
      setBusy(false);
    }
  };

  const cols = 'minmax(0,1.1fr) minmax(0,1.3fr) minmax(0,1.4fr) 40px';
  return (
    <Dialog title={t('add.title')} onClose={onClose} width={940}>
      <div style={{ marginTop: 16, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
        <button type="button" className="btn btn-well btn-sm" style={{ minHeight: 46, borderRadius: 14 }} disabled={reading} onClick={() => void pick(false)}>
          <Icon name="file" size={18} />
          {t('add.files')}
        </button>
        <button type="button" className="btn btn-well btn-sm" style={{ minHeight: 46, borderRadius: 14 }} disabled={reading} onClick={() => void pick(true)}>
          <Icon name="group" size={18} />
          {t('add.folder')}
        </button>
        <span style={{ fontSize: 13, color: 'var(--muted-2)' }}>{reading ? t('add.reading') : t('add.desktopHint')}</span>
      </div>

      {skipped > 0 && (
        <p role="status" className="note" style={{ marginTop: 12 }}>
          <Icon name="info" />
          <span>
            <strong style={{ color: 'var(--white)' }}>{t('add.skipped', { n: skipped })}</strong> {t('add.skippedDesktop')}
          </span>
        </p>
      )}

      {rows.length === 0 ? (
        <p style={{ margin: '24px 0 8px', padding: 24, borderRadius: 16, textAlign: 'center', color: 'var(--muted-2)', boxShadow: 'inset 0 0 0 1.5px rgba(255,255,255,.12)' }}>{t('add.desktopEmpty')}</p>
      ) : (
        <>
          <div style={{ marginTop: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <h2 style={{ margin: 0, fontSize: 16, fontWeight: 700 }}>{flagged.length ? t('add.toReview', { n: rows.length }) : t('add.toAdd', { n: rows.length })}</h2>
            <button type="button" className="link-btn" style={{ fontSize: 13, fontWeight: 600 }} onClick={() => setRows([])}>
              {t('add.removeAll')}
            </button>
          </div>
          <div style={{ marginTop: 8, maxHeight: '42vh', overflowY: 'auto', paddingBottom: 4, paddingRight: 10, scrollbarGutter: 'stable' }}>
            <div aria-hidden="true" style={{ display: 'grid', gridTemplateColumns: cols, columnGap: 12, paddingBottom: 6 }}>
              <span className="th">{t('add.colFile')}</span>
              <span className="th">{t('add.colName')}</span>
              <span className="th">{t('add.colArtist')}</span>
              <span />
            </div>
            {rows.map((r) => {
              const nameErr = nameProblem(r.name, MAX_SONG_NAME);
              const p = r.c.problem;
              return (
                <div
                  key={r.key}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: cols,
                    columnGap: 12,
                    alignItems: 'start',
                    padding: '10px 0',
                    paddingLeft: p ? 10 : 0,
                    boxShadow: p ? 'inset 0 1px 0 rgba(255,255,255,.08), inset 3px 0 0 var(--sand)' : 'inset 0 1px 0 rgba(255,255,255,.08)',
                  }}
                >
                  <span className="mono ellipsis" style={{ paddingTop: 12, fontSize: 12, color: 'var(--muted-2)' }} title={r.c.path}>
                    {fileName(r.c.path)}
                  </span>
                  {p ? (
                    <span role="alert" style={{ gridColumn: 'span 2', display: 'flex', gap: 8, alignItems: 'center', minHeight: 44, padding: '0 12px', borderRadius: 12, background: 'var(--sand)', color: 'var(--ink)', fontSize: 13, fontWeight: 600 }}>
                      <Icon name="alert" size={16} strokeWidth={2.4} />
                      {problemText(p)}
                    </span>
                  ) : (
                    <>
                      <input className="input compact" type="text" aria-label={t('add.colName')} value={r.name} aria-invalid={!!nameErr} onChange={(e) => update(r.key, { name: e.target.value })} />
                      <ArtistField compact label={t('add.colArtist')} value={r.artist} hint={r.c.artist_hint || null} onChange={(c) => update(r.key, { artist: c })} />
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

      <div style={{ marginTop: 16, display: 'flex', flexWrap: 'wrap', gap: 16, alignItems: 'flex-end' }}>
        <div style={{ flex: '1 1 260px' }}>
          <label htmlFor={plId} className="label-sm" style={{ display: 'block', marginBottom: 6 }}>
            {t('add.playlist')}
          </label>
          <PlaylistPicker id={plId} value={playlists} onChange={setPlaylists} />
        </div>
        <label htmlFor={syncId} style={{ flex: '1 1 260px', display: 'flex', gap: 12, alignItems: 'center', minHeight: 46, cursor: cloud ? 'pointer' : 'default', opacity: cloud ? 1 : 0.6 }}>
          <input id={syncId} type="checkbox" role="switch" checked={sync && cloud} disabled={!cloud} onChange={(e) => setSync(e.target.checked)} style={{ width: 20, height: 20, accentColor: 'var(--sand)' }} />
          <span style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <strong style={{ fontSize: 14 }}>{t('add.syncNow')}</strong>
            <span style={{ fontSize: 12, color: 'var(--muted-2)' }}>{!cloud ? t('add.syncUnavailable') : sync ? t('add.syncNoteOn') : t('add.syncNoteOff')}</span>
          </span>
        </label>
      </div>

      <p className="note" style={{ marginTop: 14 }}>
        <Icon name="info" />
        <span>{t('add.localNote')}</span>
      </p>

      <div className="dialog-actions" style={{ marginTop: 18, alignItems: 'center' }}>
        {flagged.some((r) => r.c.problem) && <span style={{ marginRight: 'auto', fontSize: 13, color: 'var(--sand)' }}>{t('add.fixFlagged')}</span>}
        <button type="button" className="btn btn-secondary btn-md" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="button" className="btn btn-primary btn-md" disabled={!ready} onClick={() => void confirm()} style={{ padding: '0 26px' }}>
          {t('add.add', { n: rows.length })}
        </button>
      </div>
    </Dialog>
  );
}
