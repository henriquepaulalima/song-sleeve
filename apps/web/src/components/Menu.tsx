import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import { createPortal } from 'react-dom';

/**
 * A popover menu anchored to a trigger. Arrow keys move between items, Escape and
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
  anchor: RefObject<HTMLElement | null>;
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

  useLayoutEffect(() => {
    const a = anchor.current?.getBoundingClientRect();
    const m = ref.current?.getBoundingClientRect();
    if (!a || !m) return;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    let left = side === 'left' ? a.left - width - 8 : align === 'end' ? a.right - width : a.left;
    if (side === 'left' && left < 8) left = Math.min(a.right + 8, vw - width - 8);
    left = Math.max(8, Math.min(left, vw - width - 8));
    let top = side === 'left' ? a.top - 6 : a.bottom + 6;
    if (top + m.height > vh - 8) top = Math.max(8, (side === 'left' ? a.bottom : a.top - 6) - m.height);
    setPos({ top, left });
  }, [anchor, align, side, width]);

  useEffect(() => {
    const el = ref.current;
    el?.querySelector<HTMLElement>('[role^="menuitem"]:not([disabled])')?.focus();
    const onDown = (e: MouseEvent) => {
      const target = e.target as Element;
      // Clicks inside a submenu (another .menu) don't close the parent.
      if (target.closest?.('.menu') || anchor.current?.contains(target)) return;
      if (!el?.contains(target)) onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (!el) return;
      const active = document.activeElement;
      // A key pressed inside a submenu belongs to the submenu.
      if (active && !el.contains(active) && active.closest('.menu')) return;
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
        anchor.current?.focus();
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
    const onScroll = () => onClose();
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    window.addEventListener('resize', onScroll);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
      window.removeEventListener('resize', onScroll);
    };
  }, [anchor, onClose]);

  return createPortal(
    <div ref={ref} role="menu" aria-label={label} className="menu" style={{ width, top: pos?.top ?? -9999, left: pos?.left ?? -9999 }}>
      {children}
    </div>,
    document.body,
  );
}

export function MenuItem({
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
      <span aria-hidden="true" className={'mi-dot' + (danger ? ' danger' : '')} />
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
