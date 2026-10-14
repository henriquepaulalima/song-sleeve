import { useState } from 'react';
import { formatClock, libraryOrder, matchesQuery, type MessageKey, type Track } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useCatalogSelect, usePlayer, useUploads } from '../app/hooks';
import { useT } from '../app/i18n';
import { player, uploads } from '../app/services';
import { EmptyState, SearchField } from '../components/common';
import { Icon } from '../components/Icon';
import { IncrementalList, ReorderableList } from '../components/ReorderableList';
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
  const dialogs = useDialogs();
  const actions = useActions();
  const [q, setQ] = useState('');
  const s = useCatalog();
  const nowPlaying = usePlayer().current?.id;
  const order = useCatalogSelect((st) => libraryOrder(st));
  const items = useUploads();
  const pendingUploads = items.filter((i) => i.status !== 'done' && !(i.trackId && s.tracks.has(i.trackId)));
  const failed = items.filter((i) => i.status === 'failed').length;
  const syncing = order.filter((x) => x.state === 'pending_upload').length;
  const queued = items.filter((i) => i.status === 'queued').length;
  const filtered = q ? order.filter((x) => matchesQuery(q, x.name, x.artist_id ? s.artists.get(x.artist_id)?.name : null)) : order;
  const ids = order.map((x) => x.id);
  const play = (id: string) => void player.playList(ids, id, { kind: 'library', label: t('source.library') });

  const addBtn = (
    <button type="button" className="btn btn-primary" onClick={() => dialogs.open({ kind: 'addSongs' })}>
      <Icon name="plus" size={18} strokeWidth={2.4} />
      {t('home.addSongs')}
    </button>
  );

  return (
    <>
      <header className="page-head">
        <div>
          <h1 className="page-title">{t('songs.title')}</h1>
          <p className="page-sub">{t('count.songs', { n: order.length })}</p>
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, minWidth: 250, padding: '8px 14px', borderRadius: 14, background: 'var(--charcoal)' }}>
            <Icon name="cloudCheck" size={20} style={{ color: 'var(--sand)' }} />
            <span style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
              <strong style={{ fontSize: 13, fontWeight: 600 }}>{t('songs.cloudStatus')}</strong>
              <span style={{ fontSize: 12, color: 'var(--muted-2)' }}>{syncing + queued + failed === 0 ? t('songs.allSynced') : t('songs.syncSummary', { syncing, queued, failed })}</span>
            </span>
          </div>
          {failed > 0 && (
            <button type="button" className="btn btn-sand" onClick={() => uploads.retryAllFailed()}>
              <Icon name="retry" size={16} strokeWidth={2.4} />
              {t('songs.retryFailed')}
            </button>
          )}
          {addBtn}
        </div>
      </header>
      <div style={{ marginTop: 20 }}>
        <SearchField label={t('songs.search')} placeholder={t('songs.search')} value={q} onChange={setQ} />
      </div>

      {order.length === 0 && pendingUploads.length === 0 ? (
        <EmptyState icon="note" title={t('state.noSongs')} body={t('state.noSongsBody')} action={addBtn} />
      ) : q && filtered.length === 0 ? (
        <EmptyState title={t('state.noResults', { query: q })} body={t('state.noResultsBody')} />
      ) : (
        <div className="table-scroll" style={{ marginTop: 18 }}>
          <div style={{ minWidth: 820, display: 'flex', flexDirection: 'column', gap: 6 }}>
            <SongTableHead grip={!q} artist />
            {pendingUploads.length > 0 && !q && (
              <ol aria-label={t('songs.uploading')} className="song-list">
                {pendingUploads.map((i) => (
                  <li key={i.id}>
                    <UploadRow item={i} />
                  </li>
                ))}
              </ol>
            )}
            {q ? (
              <IncrementalList
                items={filtered}
                getId={(x) => x.id}
                label={t('songs.listLabel')}
                render={(x) => <SongRow track={x} playing={x.id === nowPlaying} onPlay={() => play(x.id)} ctx={{}} />}
              />
            ) : (
              <ReorderableList<Track>
                items={order}
                getId={(x) => x.id}
                getName={(x) => x.name}
                enabled
                label={t('songs.listLabel')}
                onMove={(x, after, before) => actions.moveTrack(x, after, before)}
                renderRow={(x, h) => <SongRow track={x} playing={x.id === nowPlaying} onPlay={() => play(x.id)} ctx={{ grip: h.grip, moveToTop: h.moveToTop, moveToBottom: h.moveToBottom }} dragging={h.dragging} />}
              />
            )}
          </div>
        </div>
      )}
    </>
  );
}
