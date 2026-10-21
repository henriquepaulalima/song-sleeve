import { useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { Link } from 'react-router';
import { formatClock, playlistsOfTrack, songStatus, type MessageKey, type Playlist, type SongStatus, type Track } from '@song-sleeve/core';
import { useActions } from '../app/actions';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useIsPlaying, useSignal } from '../app/hooks';
import { useT } from '../app/i18n';
import { platform } from '../app/platform';
import { player, replacements } from '../app/services';
import { useSession } from '../app/session';
import { useToasts } from '../app/toasts';
import { Icon, type IconName } from './Icon';
import { Menu, MenuDivider, MenuItem, useContextMenu, type MenuAnchor } from './Menu';
import { Equalizer } from './Sleeves';

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
    case 'local':
      return { label: t('status.onDevice'), kind: 'outline', icon: 'laptop', pct: null };
    case 'queued':
      return { label: t('status.queued'), kind: 'progress', icon: 'clock', pct: null };
    case 'failed':
      return { label: t('status.failed'), kind: 'alert', icon: 'alert', pct: null };
    case 'relink':
      return { label: t('status.needsRelink'), kind: 'alert', icon: 'alert', pct: null };
    case 'changed':
      return { label: t('status.sourceChanged'), kind: 'alert', icon: 'alert', pct: null };
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
  // A click anywhere on the row plays, except on its own controls (links, buttons,
  // the grip, menus portaled from it) or when the press started on one of them.
  const pressOnControl = useRef(false);
  // The row that's playing toggles pause instead of restarting the song.
  const activate = () => (playing ? void player.toggle() : onPlay());
  const isControl = (el: EventTarget) => !!(el as Element).closest?.('a, button, input, .grip, .menu, [role="menu"]');
  const context = useContextMenu();
  return (
    <div
      ref={rowRef}
      data-track-id={track.id}
      className={'song-row clickable' + (playing ? ' playing' : '') + (dragging ? ' dragging' : '')}
      style={{ gridTemplateColumns: songColumns(!!ctx.grip, showArtist), ...style }}
      onPointerDown={(e) => (pressOnControl.current = isControl(e.target))}
      onContextMenu={context.onContextMenu}
      onClick={(e) => {
        if (pressOnControl.current || isControl(e.target) || dragging) return;
        activate();
      }}
    >
      {ctx.grip}
      <button type="button" className="song-main" onClick={activate} style={{ paddingLeft: ctx.grip ? 0 : 8 }} aria-label={`${t('common.play')}: ${track.name}`}>
        <span aria-hidden="true" className="song-thumb">
          <span className="thumb-idle">
            {playing ? <RowEqualizer /> : <Icon name="note" />}
          </span>
          <span className="thumb-hover">
            <RowHoverIcon playing={playing} />
          </span>
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
      {context.at && <SongMenu track={track} ctx={ctx} anchor={context.at} onClose={context.close} statusLabel={status.label} statusKind={status.kind} onPlay={playing ? undefined : onPlay} />}
      {status.pct != null && (
        <span aria-hidden="true" className="row-bar">
          <span style={{ width: `${status.pct}%` }} />
        </span>
      )}
    </div>
  );
}

function RowEqualizer() {
  return <Equalizer playing={useIsPlaying()} size={16} />;
}

/** Hover hint: play, or pause on the row that's playing. */
function RowHoverIcon({ playing }: { playing: boolean }) {
  return playing ? <PauseHint /> : <Icon name="play" />;
}

function PauseHint() {
  return useIsPlaying() ? <Icon name="pause" /> : <Icon name="play" />;
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
  onPlay,
}: {
  track: Track;
  ctx: RowContext;
  anchor: MenuAnchor;
  onClose: () => void;
  /** Computed from the catalog when left out. */
  statusLabel?: string;
  statusKind?: BadgeKind;
  /** Right-click menus start with Play. */
  onPlay?: () => void;
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
  const { me } = useSession();
  const toasts = useToasts();
  const local = platform.local && track.device?.local ? platform.local : null;
  const status = songStatus(track);
  const runLocal = (fn: () => Promise<void>) => go(() => void fn().catch((e) => toasts.error(e)));
  const view = statusLabel && statusKind ? { label: statusLabel, kind: statusKind } : statusView(songStatus(track, s.progress.get(track.id)), t);
  const canReplace = local ? status.kind === 'local' || status.kind === 'failed' : !pending && !replacing;
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
            <StatusBadge label={view.label} kind={view.kind} icon="info" small />
          </span>
        </span>
      </div>
      {onPlay && <MenuItem icon="play" label={t('common.play')} onSelect={go(onPlay)} />}
      {local && status.kind === 'relink' && <MenuItem icon="link" label={t('menu.relink')} sub={t('menu.relinkSub')} onSelect={runLocal(() => local.relink(track, t))} />}
      {local && status.kind === 'changed' && <MenuItem icon="check" label={t('menu.useChanged')} sub={t('menu.useChangedSub')} onSelect={runLocal(() => local.replaceSource(track, t))} />}
      {local && status.kind === 'failed' && (
        <MenuItem icon="retry" label={t('menu.retrySync')} sub={t('menu.retrySyncSub', { reason: status.reason ? t(`reason.${status.reason}` as MessageKey) : t('error.generic') })} onSelect={runLocal(() => local.retry([track.id], t))} />
      )}
      {track.metadata_suggestion && <MenuItem icon="info" label={t('menu.reviewSuggestion')} sub={t('menu.reviewSuggestionSub')} onSelect={go(() => dialogs.open({ kind: 'editSong', trackId: track.id }))} />}
      <MenuItem icon="pencil" label={t('menu.edit')} onSelect={go(() => dialogs.open({ kind: 'editSong', trackId: track.id }))} />
      <MenuItem icon="playlistAdd" label={t('menu.addToPlaylist')} chevron expanded={sub} itemRef={subAnchor} onSelect={() => setSub((v) => !v)} />
      {canReplace && <MenuItem icon="file" label={t('menu.replace')} onSelect={local ? runLocal(() => local.replaceSource(track, t)) : go(() => dialogs.open({ kind: 'replaceAudio', trackId: track.id }))} />}
      {local && status.kind === 'local' && me?.cloud_sync_enabled && <MenuItem icon="cloudUp" label={t('menu.syncToCloud')} sub={t('menu.syncToCloudSub')} onSelect={runLocal(() => local.sync([track.id], t))} />}
      {local && status.kind === 'queued' && <MenuItem icon="close" label={t('menu.cancelQueued')} sub={t('menu.cancelQueuedSub')} onSelect={runLocal(() => local.cancelQueued([track.id], t))} />}
      {local && <MenuItem icon="folderOpen" label={t('menu.showInFolder')} onSelect={runLocal(() => local.showInFolder(track, t))} />}
      {pending && <MenuItem icon="close" label={t('menu.cancelSync')} sub={t('menu.cancelSyncSub')} onSelect={go(() => void actions.cancelSync(track))} />}
      {replacing && <MenuItem icon="close" label={t('menu.cancelReplace')} sub={t('menu.cancelReplaceSub')} onSelect={go(() => void actions.cancelOperation(track))} />}
      {(ctx.moveToTop || ctx.moveToBottom) && <MenuDivider />}
      {ctx.moveToTop && <MenuItem icon="arrowTop" label={t('common.moveTop')} onSelect={go(ctx.moveToTop)} />}
      {ctx.moveToBottom && <MenuItem icon="arrowBottom" label={t('common.moveBottom')} onSelect={go(ctx.moveToBottom)} />}
      {ctx.playlist && (
        <>
          <MenuDivider />
          <MenuItem icon="minus" label={t('menu.removeFromPlaylist')} sub={t('menu.removeFromPlaylistSub')} onSelect={go(() => void actions.removeFromPlaylist(track.id, ctx.playlist!))} />
        </>
      )}
      <MenuDivider />
      <MenuItem icon="trash" label={t('menu.delete')} danger onSelect={go(() => dialogs.open({ kind: 'deleteSong', trackId: track.id }))} />
      {sub && (
        <Menu anchor={subAnchor} onClose={() => setSub(false)} label={t('menu.addToPlaylist')} width={270} side="left">
          <p className="menu-cap">{t('menu.addToPlaylistTitle')}</p>
          <MenuItem
            icon="plus"
            label={t('menu.newPlaylist')}
            onSelect={() => {
              setSub(false);
              onClose();
              dialogs.open({ kind: 'createPlaylist', addTrackId: track.id });
            }}
          />
          {playlists.length > 0 && <MenuDivider />}
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
