import { useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { Link } from 'react-router';
import { formatClock, playlistsOfTrack, songStatus, type MessageKey, type Playlist, type SongStatus, type Track } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useSignal } from '../app/hooks';
import { useT } from '../app/i18n';
import { replacements } from '../app/services';
import { Icon, type IconName } from './Icon';
import { Menu, MenuDivider, MenuItem } from './Menu';

type BadgeKind = 'quiet' | 'outline' | 'progress' | 'alert';

export function StatusBadge({ label, kind, icon, small }: { label: string; kind: BadgeKind; icon: IconName; small?: boolean }) {
  return (
    <span className={`badge badge-${kind}`} style={small ? { padding: '1px 7px', fontSize: 11, borderRadius: 6 } : undefined}>
      {!small && <Icon name={icon} />}
      {label}
    </span>
  );
}

export function statusView(s: SongStatus, t: (k: MessageKey, p?: Record<string, string | number>) => string): { label: string; kind: BadgeKind; icon: IconName; pct: number | null } {
  switch (s.kind) {
    case 'synced':
      return { label: t('status.synced'), kind: 'quiet', icon: 'cloudCheck', pct: null };
    case 'syncing':
      return { label: s.pct >= 100 ? t('status.verifying') : t('status.syncing', { pct: s.pct }), kind: 'progress', icon: 'cloudUp', pct: s.pct };
    case 'verifying':
      return { label: t('status.verifying'), kind: 'progress', icon: 'cloudUp', pct: 100 };
    case 'updating':
      return { label: t('status.updating', { pct: s.pct }), kind: 'progress', icon: 'cloudUp', pct: s.pct };
    case 'waiting':
      return { label: t('status.waiting'), kind: 'progress', icon: 'clock', pct: null };
    case 'attention':
      return { label: t('status.attention'), kind: 'alert', icon: 'alert', pct: null };
  }
}

export interface RowContext {
  /** Present inside a playlist: adds "Remove from playlist". */
  playlist?: Playlist;
  showArtist?: boolean;
  grip?: ReactNode;
  moveToTop?: () => void;
  moveToBottom?: () => void;
}

export function songColumns(grip: boolean, showArtist: boolean): string {
  return [grip ? '44px' : null, 'minmax(0,2.2fr)', showArtist ? 'minmax(0,1.3fr)' : null, '230px', '56px', '44px'].filter(Boolean).join(' ');
}

/** The shared song row (designs/WebSongRow): Songs, Playlist and Artist pages. */
export function SongRow({
  track,
  playing,
  onPlay,
  ctx,
  style,
  dragging,
  rowRef,
}: {
  track: Track;
  playing: boolean;
  onPlay: () => void;
  ctx: RowContext;
  style?: CSSProperties;
  dragging?: boolean;
  rowRef?: (el: HTMLElement | null) => void;
}) {
  const t = useT();
  const s = useCatalog();
  const local = useSignal(replacements.progress).get(track.id);
  const progress = s.progress.get(track.id);
  const merged = local != null ? { confirmed: Math.max(local, progress?.confirmed ?? 0), declared: track.operation?.declared_bytes ?? 0, at: 0 } : progress;
  const status = statusView(songStatus(track, merged), t);
  const artist = track.artist_id ? s.artists.get(track.artist_id) : undefined;
  const showArtist = ctx.showArtist ?? true;
  return (
    <div ref={rowRef} className={'song-row' + (playing ? ' playing' : '') + (dragging ? ' dragging' : '')} style={{ gridTemplateColumns: songColumns(!!ctx.grip, showArtist), ...style }}>
      {ctx.grip}
      <button type="button" className="song-main" onClick={onPlay} style={{ paddingLeft: ctx.grip ? 0 : 8 }} aria-label={`${t('common.play')}: ${track.name}`}>
        <span aria-hidden="true" className="song-thumb">
          <Icon name={playing ? 'equalizer' : 'note'} />
        </span>
        <span className="ellipsis">{track.name}</span>
      </button>
      {showArtist && (
        <span className="ellipsis" style={{ minWidth: 0 }}>
          {artist ? (
            <Link to={`/artists/${artist.id}`} className="artist-link">
              {artist.name}
            </Link>
          ) : (
            <span style={{ color: 'var(--muted-3)' }}>{t('common.noArtist')}</span>
          )}
        </span>
      )}
      <span style={{ minWidth: 0 }}>
        <StatusBadge {...status} />
      </span>
      <span className="mono" style={{ textAlign: 'right', fontSize: 13, color: 'var(--muted)' }}>
        {formatClock(track.duration_ms / 1000)}
      </span>
      <SongMenuButton track={track} ctx={ctx} statusLabel={status.label} statusKind={status.kind} />
      {status.pct != null && (
        <span aria-hidden="true" className="row-bar">
          <span style={{ width: `${status.pct}%` }} />
        </span>
      )}
    </div>
  );
}

export function SongMenuButton({ track, ctx, statusLabel, statusKind, round }: { track: Track; ctx: RowContext; statusLabel: string; statusKind: BadgeKind; round?: boolean }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button
        ref={btn}
        type="button"
        className={round ? 'round-btn' : 'icon-btn'}
        aria-label={t('common.moreActions', { name: track.name })}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        style={round && open ? { background: 'var(--well)' } : undefined}
      >
        <Icon name="more" />
      </button>
      {open && <SongMenu track={track} ctx={ctx} anchor={btn} onClose={() => setOpen(false)} statusLabel={statusLabel} statusKind={statusKind} />}
    </>
  );
}

/** Song menu: actions depend on the song's state (§8.3 Songs; designs/WebSongMenu). */
export function SongMenu({
  track,
  ctx,
  anchor,
  onClose,
  statusLabel,
  statusKind,
}: {
  track: Track;
  ctx: RowContext;
  anchor: React.RefObject<HTMLElement | null>;
  onClose: () => void;
  statusLabel: string;
  statusKind: BadgeKind;
}) {
  const t = useT();
  const dialogs = useDialogs();
  const actions = useActions();
  const s = useCatalog();
  const [sub, setSub] = useState(false);
  const subAnchor = useRef<HTMLButtonElement>(null);
  const playlists = [...s.playlists.values()].sort((a, b) => a.name.localeCompare(b.name));
  const inPlaylists = new Set(playlistsOfTrack(s, track.id).map((p) => p.id));
  const pending = track.state === 'pending_upload';
  const op = track.operation;
  const replacing = op?.kind === 'replace';
  const go = (fn: () => void) => () => {
    onClose();
    fn();
  };
  return (
    <Menu anchor={anchor} onClose={onClose} label={t('menu.actions', { name: track.name })}>
      <div className="menu-head">
        <span aria-hidden="true" className="song-thumb" style={{ width: 36, height: 36, background: 'var(--charcoal)' }}>
          <Icon name="note" size={16} />
        </span>
        <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0 }}>
          <strong className="ellipsis" style={{ fontSize: 14 }}>
            {track.name}
          </strong>
          <span style={{ alignSelf: 'flex-start' }}>
            <StatusBadge label={statusLabel} kind={statusKind === 'progress' ? 'progress' : statusKind} icon="info" small />
          </span>
        </span>
      </div>
      {track.metadata_suggestion && <MenuItem label={t('menu.reviewSuggestion')} sub={t('menu.reviewSuggestionSub')} onSelect={go(() => dialogs.open({ kind: 'editSong', trackId: track.id }))} />}
      <MenuItem label={t('menu.edit')} onSelect={go(() => dialogs.open({ kind: 'editSong', trackId: track.id }))} />
      <MenuItem label={t('menu.addToPlaylist')} chevron expanded={sub} itemRef={subAnchor} onSelect={() => setSub((v) => !v)} />
      {!pending && !replacing && <MenuItem label={t('menu.replace')} onSelect={go(() => dialogs.open({ kind: 'replaceAudio', trackId: track.id }))} />}
      {pending && <MenuItem label={t('menu.cancelSync')} sub={t('menu.cancelSyncSub')} onSelect={go(() => void actions.cancelSync(track))} />}
      {replacing && <MenuItem label={t('menu.cancelReplace')} sub={t('menu.cancelReplaceSub')} onSelect={go(() => void actions.cancelOperation(track))} />}
      {(ctx.moveToTop || ctx.moveToBottom) && <MenuDivider />}
      {ctx.moveToTop && <MenuItem label={t('common.moveTop')} onSelect={go(ctx.moveToTop)} />}
      {ctx.moveToBottom && <MenuItem label={t('common.moveBottom')} onSelect={go(ctx.moveToBottom)} />}
      {ctx.playlist && (
        <>
          <MenuDivider />
          <MenuItem label={t('menu.removeFromPlaylist')} sub={t('menu.removeFromPlaylistSub')} onSelect={go(() => void actions.removeFromPlaylist(track.id, ctx.playlist!))} />
        </>
      )}
      <MenuDivider />
      <MenuItem label={t('menu.delete')} danger onSelect={go(() => dialogs.open({ kind: 'deleteSong', trackId: track.id }))} />
      {sub && (
        <Menu anchor={subAnchor} onClose={() => setSub(false)} label={t('menu.addToPlaylist')} width={270} side="left">
          <p className="menu-cap">{t('menu.addToPlaylistTitle')}</p>
          {playlists.length === 0 && <p style={{ margin: '6px 10px 10px', fontSize: 14, color: 'var(--muted-2)' }}>{t('menu.noPlaylists')}</p>}
          {playlists.map((p) => {
            const on = inPlaylists.has(p.id);
            return (
              <button key={p.id} type="button" role="menuitemcheckbox" aria-checked={on} className="mi" style={{ fontSize: 14 }} onClick={() => void actions.toggleInPlaylist(track, p)}>
                <span aria-hidden="true" className={'checkbox-box' + (on ? ' on' : '')}>
                  {on && <Icon name="check" size={14} strokeWidth={3} />}
                </span>
                <span className="ellipsis" style={{ flex: 1 }}>
                  {p.name}
                </span>
              </button>
            );
          })}
        </Menu>
      )}
    </Menu>
  );
}
