import { Icon } from './Icon';

/** Square cover placeholder with the thin inner "sleeve" frame (Foundations). */
export function SongSleeve({ size, radius = 14, frame = 9, iconSize = 38, background = 'var(--charcoal)', children }: { size?: number | string; radius?: number; frame?: number; iconSize?: number; background?: string; children?: React.ReactNode }) {
  return (
    <span
      aria-hidden="true"
      className="sleeve"
      style={{ width: size, height: size, aspectRatio: size ? undefined : '1 / 1', borderRadius: radius, background, boxShadow: 'inset 0 1px 0 rgba(255,255,255,.08), 0 14px 24px -14px rgba(0,0,0,.75)' }}
    >
      <span className="sleeve-frame" style={{ inset: frame }} />
      <Icon name="note" size={iconSize} strokeWidth={1.8} />
      {children}
    </span>
  );
}

export function PlaylistStack({ size = 68, inner = 56 }: { size?: number; inner?: number }) {
  const off = size - inner;
  return (
    <span aria-hidden="true" style={{ position: 'relative', width: size, height: size, flex: 'none' }}>
      <span style={{ position: 'absolute', left: off, top: 0, width: inner, height: inner, borderRadius: 9, background: 'var(--raised)' }} />
      <span
        style={{
          position: 'absolute',
          left: 0,
          top: off,
          width: inner,
          height: inner,
          borderRadius: 9,
          background: 'var(--well)',
          color: 'var(--sand)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          boxShadow: 'inset 0 0 0 1px rgba(255,255,255,.1)',
        }}
      >
        <Icon name="songs" size={Math.round(inner * 0.4)} />
      </span>
    </span>
  );
}

export function GroupStack({ scale = 1 }: { scale?: number }) {
  const s = (n: number) => n * scale;
  return (
    <span aria-hidden="true" style={{ position: 'relative', width: s(68), height: s(68), flex: 'none' }}>
      <span style={{ position: 'absolute', left: s(12), top: 0, width: s(52), height: s(52), borderRadius: 9, background: 'var(--raised-2)', transform: 'rotate(10deg)' }} />
      <span style={{ position: 'absolute', left: s(6), top: s(6), width: s(52), height: s(52), borderRadius: 9, background: 'var(--raised)', transform: 'rotate(4deg)' }} />
      <span
        style={{
          position: 'absolute',
          left: 0,
          top: s(14),
          width: s(54),
          height: s(54),
          borderRadius: 9,
          background: 'var(--well)',
          color: 'var(--sand)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          boxShadow: 'inset 0 0 0 1px rgba(255,255,255,.1)',
        }}
      >
        <Icon name="group" size={s(22)} />
      </span>
    </span>
  );
}

/** Large header art for playlist detail. */
export function PlaylistHero() {
  return (
    <span aria-hidden="true" style={{ position: 'relative', width: 160, height: 160, flex: 'none' }}>
      <span style={{ position: 'absolute', left: 22, top: 0, width: 138, height: 138, borderRadius: 16, background: 'var(--raised)' }} />
      <span
        style={{
          position: 'absolute',
          left: 0,
          top: 22,
          width: 138,
          height: 138,
          borderRadius: 16,
          background: 'var(--charcoal)',
          color: 'var(--sand)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          boxShadow: 'inset 0 1px 0 rgba(255,255,255,.1), 0 24px 36px -18px rgba(0,0,0,.85)',
        }}
      >
        <span className="sleeve-frame" style={{ inset: 10 }} />
        <Icon name="songs" size={44} strokeWidth={1.8} />
      </span>
    </span>
  );
}

export function GroupHero() {
  return (
    <span aria-hidden="true" style={{ position: 'relative', width: 168, height: 168, flex: 'none' }}>
      <span style={{ position: 'absolute', left: 30, top: 0, width: 128, height: 128, borderRadius: 16, background: 'var(--raised-2)', transform: 'rotate(10deg)' }} />
      <span style={{ position: 'absolute', left: 16, top: 12, width: 128, height: 128, borderRadius: 16, background: 'var(--raised)', transform: 'rotate(4deg)' }} />
      <span
        style={{
          position: 'absolute',
          left: 0,
          top: 28,
          width: 136,
          height: 136,
          borderRadius: 16,
          background: 'var(--charcoal)',
          color: 'var(--sand)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          boxShadow: 'inset 0 1px 0 rgba(255,255,255,.1), 0 24px 36px -18px rgba(0,0,0,.85)',
        }}
      >
        <Icon name="group" size={44} strokeWidth={1.8} />
      </span>
    </span>
  );
}
