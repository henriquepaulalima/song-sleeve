import { useEffect } from 'react';
import { useSearchParams } from 'react-router';
import { formatClock, libraryOrder, type MessageKey, type Track } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useCatalog, useCatalogSelect, useCurrentTrackId, useUploads } from '../app/hooks';
import { useT } from '../app/i18n';
import { player, uploads } from '../app/services';
import { EmptyState } from '../components/common';
import { Icon } from '../components/Icon';
import { AddSongsButton, PageHeader } from '../components/PageHeader';
import { SyncControl } from '../components/SyncControl';
import { ReorderableList } from '../components/ReorderableList';
import { SongRow, StatusBadge, songColumns } from '../components/SongRow';
import type { UploadItem } from '../upload/uploader';

export function SongTableHead({ grip, artist }: { grip: boolean; artist: boolean }) {
  const t = useT();
  return (
    <div aria-hidden="true" className="grid-head" style={{ gridTemplateColumns: songColumns(grip, artist) }}>
      {grip && <span />}
      <span className="th">{t('songs.colTitle')}</span>
      {artist && <span className="th">{t('songs.colArtist')}</span>}
      <span className="th">{t('songs.colStatus')}</span>
      <span className="th" style={{ textAlign: 'right' }}>
        {t('songs.colTime')}
      </span>
      <span />
    </div>
  );
}

/** A song this tab is uploading that has no track document yet, or whose sync failed (R-CLOUD-9). */
function UploadRow({ item }: { item: UploadItem }) {
  const t = useT();
  const reason = item.reason ? t(`reason.${item.reason}` as MessageKey) : '';
  const label = item.status === 'failed' ? t('sync.failed', { reason }) : item.status === 'queued' ? t('status.queued') : t('status.syncing', { pct: Math.floor((item.confirmed / item.facts.byteSize) * 100) });
  return (
    <div className="song-row" style={{ gridTemplateColumns: songColumns(false, true) }}>
      <span className="song-main" style={{ paddingLeft: 8, cursor: 'default' }}>
        <span aria-hidden="true" className="song-thumb">
          <Icon name={item.status === 'failed' ? 'alert' : 'cloudUp'} />
        </span>
        <span className="ellipsis">{item.name}</span>
      </span>
      <span className="ellipsis" style={{ color: 'var(--muted)' }}>
        {item.artist?.name ?? ''}
      </span>
      <span style={{ minWidth: 0 }}>
        <StatusBadge label={label} kind={item.status === 'failed' ? 'alert' : 'progress'} icon={item.status === 'failed' ? 'alert' : item.status === 'queued' ? 'clock' : 'cloudUp'} />
      </span>
      <span className="mono" style={{ textAlign: 'right', fontSize: 13, color: 'var(--muted)' }}>
        {formatClock(item.facts.durationMs / 1000)}
      </span>
      <span style={{ display: 'flex' }}>
        {item.status === 'failed' ? (
          <button type="button" className="icon-btn" aria-label={t('common.tryAgain')} title={t('common.tryAgain')} onClick={() => uploads.retry(item.id)}>
            <Icon name="retry" size={18} />
          </button>
        ) : item.status === 'queued' ? (
          <button type="button" className="icon-btn" aria-label={t('menu.cancelSync')} title={t('menu.cancelSync')} onClick={() => uploads.cancelQueued(item.id)}>
            <Icon name="close" size={18} />
          </button>
        ) : null}
      </span>
      {item.status === 'failed' && (
        <button type="button" className="sr-only" onClick={() => uploads.dismiss(item.id)}>
          {t('common.close')}
        </button>
      )}
    </div>
  );
}

/** designs/WebSongs: every song in library order, synced songs only on the web. */
export default function Songs() {
  const t = useT();
  const actions = useActions();
  const s = useCatalog();
  const nowPlaying = useCurrentTrackId();
  const order = useCatalogSelect((st) => libraryOrder(st));
  const items = useUploads();
  const pendingUploads = items.filter((i) => i.status !== 'done' && !(i.trackId && s.tracks.has(i.trackId)));
  const webFailed = items.filter((i) => i.status === 'failed').length;
  const syncing = order.filter((x) => x.state === 'pending_upload').length;
  // Installed apps: songs only on this device, and their queued or failed syncs.
  const localOnly = order.filter((x) => x.device?.local);
  const failed = webFailed + localOnly.filter((x) => x.device?.sync_state === 'sync_failed' || x.device?.sync_state === 'needs_attention').length;
  const queued = items.filter((i) => i.status === 'queued').length + localOnly.filter((x) => x.device?.sync_state === 'queued').length;
  const ids = order.map((x) => x.id);
  const play = (id: string) => void player.playList(ids, id, { kind: 'songs', label: t('source.library') });

  // Opened from search with ?focus=<id>: scroll to that song and flash it once.
  const [params, setParams] = useSearchParams();
  const focus = params.get('focus');
  useEffect(() => {
    if (!focus) return;
    // The list may render the row a frame or two later (it grows to include it).
    let frame = 0;
    let tries = 0;
    const tick = () => {
      const row = document.querySelector<HTMLElement>(`[data-track-id="${CSS.escape(focus)}"]`);
      if (!row) {
        if (tries++ < 30) frame = requestAnimationFrame(tick);
        return;
      }
      row.scrollIntoView({ block: 'center', behavior: 'smooth' });
      row.classList.remove('flash');
      void row.offsetWidth;
      row.classList.add('flash');
      setParams({}, { replace: true });
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [focus, order.length, setParams]);

  const addBtn = <AddSongsButton />;
  const sub = [t('count.songs', { n: order.length }), localOnly.length ? t('songs.localOnly', { n: localOnly.length }) : null, syncing + queued + failed > 0 ? t('songs.syncSummary', { syncing, queued, failed }) : null].filter(Boolean).join(' · ');

  return (
    <>
      <PageHeader
        title={t('songs.title')}
        sub={sub}
        actions={
          <>
            {webFailed > 0 && (
              <button type="button" className="btn btn-sand" onClick={() => uploads.retryAllFailed()}>
                <Icon name="retry" size={16} strokeWidth={2.4} />
                {t('songs.retryFailed')}
              </button>
            )}
            <SyncControl spec={{ scope: 'library' }} />
            {addBtn}
          </>
        }
      />

      {order.length === 0 && pendingUploads.length === 0 ? (
        <EmptyState icon="note" title={t('state.noSongs')} body={t('state.noSongsBody')} action={addBtn} />
      ) : (
        <div className="table-scroll" style={{ marginTop: 22 }}>
          <div style={{ minWidth: 820, display: 'flex', flexDirection: 'column', gap: 6 }}>
            <SongTableHead grip artist />
            {pendingUploads.length > 0 && (
              <ol aria-label={t('songs.uploading')} className="song-list">
                {pendingUploads.map((i) => (
                  <li key={i.id}>
                    <UploadRow item={i} />
                  </li>
                ))}
              </ol>
            )}
            <ReorderableList<Track>
              items={order}
              revealId={focus}
              getId={(x) => x.id}
              getName={(x) => x.name}
              enabled
              label={t('songs.listLabel')}
              onMove={(x, after, before) => actions.moveTrack(x, after, before)}
              renderRow={(x, h) => <SongRow track={x} playing={x.id === nowPlaying} onPlay={() => play(x.id)} ctx={{ grip: h.grip, moveToTop: h.moveToTop, moveToBottom: h.moveToBottom }} dragging={h.dragging} />}
            />
          </div>
        </div>
      )}
    </>
  );
}
