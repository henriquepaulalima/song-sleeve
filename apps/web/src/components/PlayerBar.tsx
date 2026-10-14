import { useId } from 'react';
import { Link } from 'react-router';
import { formatClock, type PlayerState } from '@song-sleeve/core';
import { usePlayer } from '../app/hooks';
import { useT } from '../app/i18n';
import { player } from '../app/services';
import { Icon } from './Icon';

export function sourceLabel(s: PlayerState, t: ReturnType<typeof useT>): string {
  return s.source?.label ?? t('source.library');
}

/** A seek bar: elapsed and total time, disabled until the duration is known (§8.3). */
export function SeekBar({ state, big }: { state: PlayerState; big?: boolean }) {
  const t = useT();
  const id = useId();
  const loading = state.status === 'loading';
  const empty = state.status === 'empty';
  const lp = state.loadProgress;
  const pct = loading ? (lp && lp.total ? (lp.loaded / lp.total) * 100 : 0) : state.durationSec > 0 ? (state.positionSec / state.durationSec) * 100 : 0;
  const disabled = empty || loading || !(state.durationSec > 0);
  return (
    <div className="seekbar" style={big ? { maxWidth: 'none', flexDirection: 'column', alignItems: 'stretch', gap: 8, fontSize: 14 } : undefined}>
      {!big && <span>{empty ? '0:00' : formatClock(state.positionSec)}</span>}
      <span className={'range' + (big ? ' big-range' : '')}>
        <span className="range-track" />
        <span className={'range-fill' + (loading ? ' loading' : '')} style={{ width: `${Math.min(100, pct)}%` }} />
        {!loading && !empty && <span className="range-knob" style={{ left: `${Math.min(100, pct)}%` }} />}
        <label htmlFor={id} className="sr-only">
          {t('playing.seek')}
        </label>
        <input
          id={id}
          type="range"
          min={0}
          max={Math.max(1, Math.floor(state.durationSec))}
          step={1}
          value={Math.floor(state.positionSec)}
          disabled={disabled}
          onChange={(e) => player.seek(Number(e.target.value))}
          aria-valuetext={`${formatClock(state.positionSec)} / ${formatClock(state.durationSec)}`}
        />
      </span>
      {big ? (
        <span style={{ display: 'flex', justifyContent: 'space-between' }}>
          <span>{empty ? '0:00' : formatClock(state.positionSec)}</span>
          <span>{empty ? '--:--' : formatClock(state.durationSec)}</span>
        </span>
      ) : (
        <span>{empty ? '--:--' : formatClock(state.durationSec)}</span>
      )}
    </div>
  );
}

/** The persistent player bar in the app shell (designs/WebPlayer). */
export function PlayerBar() {
  const t = useT();
  const s = usePlayer();
  const empty = s.status === 'empty';
  const loading = s.status === 'loading';
  const playing = s.status === 'playing';
  const cur = s.current;
  const title = cur?.name ?? t('player.nothing');
  const sub = cur ? (cur.artist ? t('player.from', { artist: cur.artist, source: sourceLabel(s, t) }) : t('player.fromNoArtist', { source: sourceLabel(s, t) })) : t('player.pick');
  const lp = s.loadProgress;
  const pct = lp && lp.total ? Math.floor((lp.loaded / lp.total) * 100) : 0;
  return (
    <div className="pbar">
      <Link to="/playing" className="pbar-now" aria-label={t('player.open', { name: title })}>
        <span className="pbar-sleeve" style={{ color: empty ? 'var(--disabled-fg)' : 'var(--sand)' }}>
          <Icon name="note" />
        </span>
        <span style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
          <span className="ellipsis" style={{ fontSize: 16, fontWeight: 600 }}>
            {title}
          </span>
          <span className="ellipsis" style={{ fontSize: 13, color: 'var(--muted-2)' }}>
            {sub}
          </span>
        </span>
      </Link>
      <div className="pbar-ctl">
        <div className="pbar-btns">
          <button type="button" className={'icon-btn hide-narrow' + (s.shuffle && !empty ? ' toggle-on' : '')} aria-label={t('player.shuffle')} aria-pressed={s.shuffle} disabled={empty} onClick={() => player.setShuffle(!s.shuffle)}>
            <Icon name="shuffle" size={20} />
          </button>
          <button type="button" className="icon-btn" aria-label={t('player.previous')} disabled={empty} onClick={() => void player.previous()}>
            <Icon name="prev" />
          </button>
          <button type="button" className="pp" style={{ width: 52, height: 52 }} aria-label={playing ? t('player.pause') : t('player.play')} disabled={empty || loading} onClick={() => void player.toggle()}>
            {playing ? <Icon name="pause" /> : <Icon name="play" style={{ marginLeft: 3 }} />}
          </button>
          <button type="button" className="icon-btn" aria-label={t('player.next')} disabled={empty || !player.hasNext()} onClick={() => void player.next()}>
            <Icon name="next" />
          </button>
          <button type="button" className={'icon-btn hide-narrow' + (s.repeatOne && !empty ? ' toggle-on' : '')} aria-label={t('player.repeat')} aria-pressed={s.repeatOne} disabled={empty} onClick={() => player.toggleRepeat()}>
            <Icon name="repeatOne" size={20} />
          </button>
        </div>
        <SeekBar state={s} />
      </div>
      <div className="pbar-side">
        {s.notice && (
          <span role="status" className="pbar-notice">
            <Icon name="clock" />
            {t(s.notice === 'updated' ? 'notice.updated' : 'notice.removed')}
          </span>
        )}
        {s.status === 'error' && (
          <span role="alert" className="pbar-notice">
            <Icon name="alert" />
            {t('player.error')}
          </span>
        )}
        {loading && (
          <>
            <span role="status" className="dl-pill">
              <Icon name="download" />
              {t('player.downloading', { pct })}
            </span>
            <button type="button" className="btn btn-secondary btn-sm" onClick={() => player.cancelLoad()}>
              {t('common.cancel')}
            </button>
          </>
        )}
        <Link to="/playing" className="icon-btn" aria-label={t('player.openFull')} style={{ background: 'var(--well)' }}>
          <Icon name="expand" size={18} />
        </Link>
      </div>
    </div>
  );
}
