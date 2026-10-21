import { useCallback, useEffect, useLayoutEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { Icon, type IconName } from './Icon';

/** A trigger element, or a point (a right-click). */
export type MenuAnchor = RefObject<HTMLElement | null> | { x: number; y: number };

function anchorRect(anchor: MenuAnchor): { left: number; right: number; top: number; bottom: number } | undefined {
  if ('x' in anchor) return { left: anchor.x, right: anchor.x, top: anchor.y, bottom: anchor.y };
  return anchor.current?.getBoundingClientRect();
}

function anchorEl(anchor: MenuAnchor): HTMLElement | null {
  return 'x' in anchor ? null : anchor.current;
}

/**
 * A popover menu anchored to a trigger or opened at a point. Arrow keys move between items, Escape and
 * outside clicks close it, and focus returns to the trigger.
 */
export function Menu({
  anchor,
  onClose,
  label,
  children,
  width = 300,
  align = 'end',
  side,
}: {
  anchor: MenuAnchor;
  onClose: () => void;
  label: string;
  children: ReactNode;
  width?: number;
  align?: 'start' | 'end';
  /** Opens beside the anchor (submenus) instead of below it. */
  side?: 'left';
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);
  // Callers often pass a new onClose each render; the listeners (and the initial
  // focus) must not be set up again when the parent re-renders, e.g. on a toggle.
  const closeRef = useRef(onClose);
  closeRef.current = onClose;

  useLayoutEffect(() => {
    const a = anchorRect(anchor);
    const point = 'x' in anchor;
    const m = ref.current?.getBoundingClientRect();
    if (!a || !m) return;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    let left = side === 'left' ? a.left - width - 8 : align === 'end' && !point ? a.right - width : a.left;
    if (side === 'left' && left < 8) left = Math.min(a.right + 8, vw - width - 8);
    left = Math.max(8, Math.min(left, vw - width - 8));
    let top = side === 'left' ? a.top - 6 : point ? a.bottom : a.bottom + 6;
    if (top + m.height > vh - 8) top = Math.max(8, (side === 'left' ? a.bottom : a.top - 6) - m.height);
    setPos({ top, left });
  }, [anchor, align, side, width]);

  useEffect(() => {
    const el = ref.current;
    el?.querySelector<HTMLElement>('[role^="menuitem"]:not([disabled])')?.focus();
    const onDown = (e: MouseEvent) => {
      const target = e.target as Element;
      // Clicks inside a submenu (another .menu) don't close the parent.
      if (target.closest?.('.menu') || anchorEl(anchor)?.contains(target)) return;
      if (!el?.contains(target)) closeRef.current();
    };
    const onKey = (e: KeyboardEvent) => {
      if (!el) return;
      const active = document.activeElement;
      // A key pressed inside a submenu belongs to the submenu.
      if (active && !el.contains(active) && active.closest('.menu')) return;
      if (e.key === 'Escape') {
        e.stopPropagation();
        closeRef.current();
        anchorEl(anchor)?.focus();
        return;
      }
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        const items = [...el.querySelectorAll<HTMLElement>('[role^="menuitem"]:not([disabled])')];
        if (!items.length || !el.contains(document.activeElement)) return;
        e.preventDefault();
        const i = items.indexOf(document.activeElement as HTMLElement);
        const n = e.key === 'ArrowDown' ? (i + 1) % items.length : (i - 1 + items.length) % items.length;
        items[n]?.focus();
      }
    };
    const onScroll = () => closeRef.current();
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    window.addEventListener('resize', onScroll);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
      window.removeEventListener('resize', onScroll);
    };
  }, [anchor]);

  return createPortal(
    <div ref={ref} role="menu" aria-label={label} className="menu" style={{ width, top: pos?.top ?? -9999, left: pos?.left ?? -9999 }}>
      {children}
    </div>,
    document.body,
  );
}

export function MenuItem({
  icon,
  label,
  sub,
  onSelect,
  danger,
  disabled,
  badge,
  chevron,
  expanded,
  itemRef,
}: {
  /** Drawn before the label; items without one keep the dot. */
  icon?: IconName;
  label: string;
  sub?: string;
  onSelect: () => void;
  danger?: boolean;
  disabled?: boolean;
  badge?: ReactNode;
  chevron?: boolean;
  expanded?: boolean;
  itemRef?: RefObject<HTMLButtonElement | null>;
}) {
  return (
    <button
      ref={itemRef}
      type="button"
      role="menuitem"
      className="mi"
      disabled={disabled}
      aria-haspopup={chevron ? 'menu' : undefined}
      aria-expanded={chevron ? !!expanded : undefined}
      onClick={onSelect}
    >
      {icon ? (
        <Icon name={icon} size={18} className={'mi-icon' + (danger ? ' danger' : '')} />
      ) : (
        <span aria-hidden="true" className={'mi-dot' + (danger ? ' danger' : '')} />
      )}
      <span className="mi-text">
        <span>{label}</span>
        {sub && <span className="mi-sub">{sub}</span>}
      </span>
      {badge}
      {chevron && (
        <svg className="i" viewBox="0 0 24 24" aria-hidden="true" style={{ width: 16, height: 16, color: 'var(--muted-2)' }}>
          <path d="m15 18-6-6 6-6" />
        </svg>
      )}
    </button>
  );
}

export function MenuDivider() {
  return <div aria-hidden="true" className="menu-div" />;
}

/**
 * Right-click opens an item's own menu at the pointer. Text fields keep the
 * native menu so copy and paste still work.
 */
export function useContextMenu(): { at: { x: number; y: number } | null; onContextMenu: (e: ReactMouseEvent) => void; close: () => void } {
  const [at, setAt] = useState<{ x: number; y: number } | null>(null);
  const onContextMenu = useCallback((e: ReactMouseEvent) => {
    if ((e.target as Element).closest?.('input, textarea, [contenteditable="true"]')) return;
    e.preventDefault();
    e.stopPropagation();
    setAt({ x: e.clientX, y: e.clientY });
  }, []);
  const close = useCallback(() => setAt(null), []);
  return { at, onContextMenu, close };
}
