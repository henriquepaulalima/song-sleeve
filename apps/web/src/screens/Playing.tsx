import { Link, useNavigate } from 'react-router';
import { formatBytes, sourceHref } from '@song-sleeve/core';
import { useCatalog, usePlayer } from '../app/hooks';
import { useI18n } from '../app/i18n';
import { player } from '../app/services';
import { closeFull, tipFor } from '../app/shortcuts';
import { Icon } from '../components/Icon';
import { SeekBar } from '../components/PlayerBar';
import { SongMenuButton, statusView } from '../components/SongRow';
import { songStatus } from '@song-sleeve/core';

/** designs/WebPlaying: the full player. Navigating never reloads the media (§8.4). */
export default function Playing() {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const s = usePlayer();
  const cat = useCatalog();
  const empty = s.status === 'empty';
  const loading = s.status === 'loading';
  const playing = s.status === 'playing';
  const cur = s.current;
  const track = cur ? cat.tracks.get(cur.id) : undefined;
  const artistId = track?.artist_id;
  const lp = s.loadProgress;
  return (
    <div className="player-page">
      <header style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16 }}>
        <span style={{ width: 48 }} />
        <div style={{ textAlign: 'center', minWidth: 0 }}>
          <div className="kicker" style={{ color: 'var(--muted-2)' }}>
            {t('playing.from')}
          </div>
          {empty ? (
            <span style={{ display: 'inline-block', marginTop: 2, fontSize: 16, fontWeight: 600 }}>{t('playing.nothingQueued')}</span>
          ) : (
            <Link to={sourceHref(s.source)} className="ellipsis" style={{ display: 'inline-block', marginTop: 2, fontSize: 16, fontWeight: 600, color: 'var(--white)', textUnderlineOffset: 3, maxWidth: '60vw' }}>
              {s.source?.label ?? t('source.library')}
            </Link>
          )}
        </div>
        {track ? (
          <SongMenuButton track={track} ctx={{}} round {...(() => { const v = statusView(songStatus(track, cat.progress.get(track.id)), t); return { statusLabel: v.label, statusKind: v.kind }; })()} />
        ) : (
          <span style={{ width: 48 }} />
        )}
      </header>

      <div style={{ flex: 1, display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'center', gap: '40px 72px', marginTop: 28 }}>
        <div role="img" aria-label={t('playing.cover')} className="cover" style={{ color: empty ? 'var(--disabled-fg)' : 'var(--sand)' }}>
          <Icon name="note" size={96} strokeWidth={1.5} />
        </div>
        <div style={{ flex: '0 1 460px', minWidth: 0, display: 'flex', flexDirection: 'column', alignItems: 'stretch' }}>
          <h1 className="page-title" style={{ fontSize: 44, overflowWrap: 'anywhere' }}>
            {cur?.name ?? t('player.nothing')}
          </h1>
          {cur && cur.artist && artistId && (
            <Link to={`/artists/${artistId}`} style={{ alignSelf: 'flex-start', marginTop: 8, fontSize: 18, color: 'var(--muted)', textDecorationColor: 'rgba(217,213,202,.35)', textUnderlineOffset: 4 }}>
              {cur.artist}
            </Link>
          )}
          {empty && <p style={{ margin: '10px 0 0', fontSize: 16, color: 'var(--muted-2)' }}>{t('playing.choose')}</p>}
          {s.notice && (
            <p role="status" className="note" style={{ marginTop: 18, fontSize: 14, alignItems: 'flex-start' }}>
              <span style={{ display: 'flex' }}>
                <svg className="i" viewBox="0 0 24 24" aria-hidden="true" style={{ width: 18, height: 18, color: 'var(--sand)' }}>
                  <circle cx="12" cy="12" r="9" />
                  <path d="M12 7v5l3 2" />
                </svg>
              </span>
              {t(s.notice === 'updated' ? 'notice.updated' : 'notice.removed')}
            </p>
          )}
          {s.status === 'error' && (
            <p role="alert" className="alert" style={{ marginTop: 18 }}>
              {t(s.error === 'unsupported_codec' ? 'player.codecMissing' : 'player.error')}
            </p>
          )}
          <div style={{ marginTop: 26 }}>
            {loading ? (
              <div role="status" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <div aria-hidden="true" style={{ height: 8, borderRadius: 8, background: 'var(--track)', overflow: 'hidden' }}>
                  <div className="range-fill loading" style={{ position: 'static', height: '100%', width: `${lp && lp.total ? (lp.loaded / lp.total) * 100 : 0}%` }} />
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, fontSize: 14, color: 'var(--muted)' }}>
                  <span>{t('playing.loadingWeb')}</span>
                  <span className="mono">{lp ? `${formatBytes(locale, lp.loaded)} / ${formatBytes(locale, lp.total)}` : ''}</span>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                  <p style={{ margin: 0, fontSize: 13, color: 'var(--muted-2)' }}>{t('playing.cancelHint')}</p>
                  <button type="button" className="btn btn-secondary btn-sm" onClick={() => player.cancelLoad()}>
                    {t('common.cancel')}
                  </button>
                </div>
              </div>
            ) : (
              <SeekBar state={s} big />
            )}
          </div>

          <div className="dial">
            <button type="button" className={'db' + (s.shuffle && !empty ? ' toggle-on' : '')} style={{ left: 102, top: 8 }} aria-pressed={s.shuffle} aria-label={t('player.shuffle')} data-tip={tipFor('shuffle', t)} disabled={empty} onClick={() => player.setShuffle(!s.shuffle)}>
              <Icon name="shuffle" />
            </button>
            <button type="button" className="db" style={{ left: 8, top: 102 }} aria-label={t('player.previous')} data-tip={tipFor('previous', t)} disabled={empty} onClick={() => void player.previous()}>
              <Icon name="prev" size={28} />
            </button>
            <button type="button" className="db" style={{ right: 8, top: 102 }} aria-label={t('player.next')} data-tip={tipFor('next', t)} disabled={empty || !player.hasNext()} onClick={() => void player.next()}>
              <Icon name="next" size={28} />
            </button>
            <button type="button" className={'db' + (s.repeatOne && !empty ? ' toggle-on' : '')} style={{ left: 102, bottom: 8 }} aria-pressed={s.repeatOne} aria-label={t('player.repeat')} data-tip={tipFor('repeat', t)} data-tip-pos="below" disabled={empty} onClick={() => player.toggleRepeat()}>
              <Icon name="repeatOne" />
            </button>
            <button type="button" className="pp" aria-label={playing ? t('player.pause') : t('player.play')} data-tip={tipFor('playPause', t, playing ? t('player.pause') : t('player.play'))} disabled={empty || loading} onClick={() => void player.toggle()}>
              {playing ? <Icon name="pause" size={36} /> : <Icon name="play" size={36} />}
            </button>
          </div>
        </div>
      </div>
      <button type="button" className="icon-btn pbar-expand player-min" aria-label={t('playing.close')} data-tip={tipFor('closeFull', t, t('playing.close'))} data-tip-align="end" onClick={() => closeFull(navigate)}>
        <Icon name="collapse" size={18} />
      </button>
    </div>
  );
}
