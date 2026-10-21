import type { CSSProperties, ReactNode } from 'react';
import { Icon } from './Icon';

/** Three bars for the song that's playing; they move only while it plays (prefers-reduced-motion stops them). */
export function Equalizer({ playing, size = 18 }: { playing: boolean; size?: number }) {
  return (
    <span aria-hidden="true" className={'eq' + (playing ? ' on' : '')} style={{ height: size, gap: Math.max(2, Math.round(size / 7)) }}>
      <b style={{ width: Math.max(3, Math.round(size / 5)) }} />
      <b style={{ width: Math.max(3, Math.round(size / 5)) }} />
      <b style={{ width: Math.max(3, Math.round(size / 5)) }} />
    </span>
  );
}

/**
 * Square cover placeholder with the thin inner "sleeve" frame (Foundations). The
 * current song turns sand with an equalizer; `playing` is the player status.
 */
export function SongSleeve({
  size,
  radius = 14,
  frame = 9,
  iconSize = 38,
  background = 'var(--charcoal)',
  current,
  playing,
  children,
}: {
  size?: number | string;
  radius?: number;
  frame?: number;
  iconSize?: number;
  background?: string;
  current?: boolean;
  playing?: boolean;
  children?: ReactNode;
}) {
  return (
    <span
      aria-hidden="true"
      className={'sleeve' + (current ? ' current' : '')}
      style={{ width: size, height: size, aspectRatio: size ? undefined : '1 / 1', borderRadius: radius, background: current ? 'var(--sand)' : background, boxShadow: 'inset 0 1px 0 rgba(255,255,255,.08), 0 14px 24px -14px rgba(0,0,0,.75)' }}
    >
      {!current && <span className="sleeve-frame" style={{ inset: frame }} />}
      {current ? <Equalizer playing={!!playing} size={Math.round(iconSize * 0.8)} /> : <Icon name="note" size={iconSize} strokeWidth={1.8} />}
      {children}
    </span>
  );
}

/**
 * A playlist: a 2×2 mosaic of song tiles, all one colour. Tiles past the song count
 * are plain (designs/proposals/LibraryCards).
 */
export function PlaylistArt({ width, height, songs, radius = 12, style }: { width: number | string; height?: number | string; songs: number; radius?: number; style?: CSSProperties }) {
  const small = typeof width === 'number' && width < 90;
  const inner = Math.max(3, Math.round(radius * 0.6));
  return (
    <span
      aria-hidden="true"
      className="pl-art"
      style={{ width, height: height ?? width, gap: small ? 3 : 5, ['--r' as string]: `${radius}px`, ['--ri' as string]: `${inner}px`, ...style }}
    >
      {[0, 1, 2, 3].map((i) => (
        <span key={i}>{i < songs && <Icon name="note" size={small ? 11 : typeof width === 'number' && width >= 140 ? 26 : 18} />}</span>
      ))}
    </span>
  );
}

/** A group: a plain folder, with the playlist count on its front when given. */
export function GroupArt({ width, height, label, radius = 12, style }: { width: number | string; height: number | string; label?: string; radius?: number; style?: CSSProperties }) {
  return (
    <span aria-hidden="true" className="folder-art" style={{ width, height, ['--r' as string]: `${radius}px`, ...style }}>
      <span className="fa-tab" />
      <span className="fa-back" />
      <span className="fa-front">{label && <span className="fa-count">{label}</span>}</span>
    </span>
  );
}

/** Small playlist art for rows and Home cards. */
export function PlaylistStack({ size = 68, songs = 4 }: { size?: number; songs?: number }) {
  return <PlaylistArt width={size} songs={songs} radius={Math.round(size / 6)} style={{ flex: 'none' }} />;
}

/** Small group art for Home cards. */
export function GroupStack({ size = 68 }: { size?: number }) {
  return (
    <span aria-hidden="true" style={{ width: size, height: size, flex: 'none', display: 'flex', alignItems: 'center' }}>
      <GroupArt width={size} height={Math.round(size * 0.82)} radius={Math.round(size / 7)} />
    </span>
  );
}

/** Large header art for playlist detail. */
export function PlaylistHero({ songs }: { songs: number }) {
  return <PlaylistArt width={160} songs={songs} radius={18} style={{ flex: 'none', filter: 'drop-shadow(0 18px 24px rgba(0,0,0,.45))' }} />;
}

/** Large header art for group detail. */
export function GroupHero({ label }: { label?: string }) {
  return <GroupArt width={168} height={140} radius={18} label={label} style={{ flex: 'none', filter: 'drop-shadow(0 18px 24px rgba(0,0,0,.45))' }} />;
}
