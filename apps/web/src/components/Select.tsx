import { useRef, useState, type ReactNode } from 'react';
import { Icon } from './Icon';
import { Menu } from './Menu';

export interface SelectOption {
  value: string;
  label: string;
  lang?: string;
}

/**
 * A dropdown drawn by the app instead of a native <select>. WebKitGTK shows a
 * native select's list as a separate system window outside the app (and unstyled),
 * so every picker uses this, built on the shared Menu popover.
 */
export function Select({
  id,
  value,
  options,
  onChange,
  label,
  className = 'input compact',
  extra,
  minWidth = 200,
  ariaLabel,
}: {
  id?: string;
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  /** Accessible name of the list. */
  label: string;
  className?: string;
  /** Items after the options (for example "New playlist…"); receives a close function. */
  extra?: (close: () => void) => ReactNode;
  minWidth?: number;
  /** For a picker without a visible <label>. */
  ariaLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  const current = options.find((o) => o.value === value);
  const width = Math.max(minWidth, btn.current?.offsetWidth ?? 0);
  const close = () => setOpen(false);
  return (
    <>
      <button
        ref={btn}
        id={id}
        type="button"
        className={className + ' select-btn'}
        aria-label={ariaLabel && current ? `${ariaLabel}: ${current.label}` : ariaLabel}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' && !open) {
            e.preventDefault();
            setOpen(true);
          }
        }}
      >
        <span className="ellipsis" lang={current?.lang}>
          {current?.label ?? ''}
        </span>
        <Icon name="chevronDown" size={16} />
      </button>
      {open && (
        <Menu anchor={btn} onClose={close} label={label} width={width} align="start">
          <div style={{ maxHeight: 300, overflowY: 'auto' }}>
            {options.map((o) => (
              <button
                key={o.value}
                type="button"
                role="menuitemradio"
                aria-checked={o.value === value}
                className="mi"
                lang={o.lang}
                onClick={() => {
                  onChange(o.value);
                  close();
                  btn.current?.focus();
                }}
              >
                <span className="ellipsis" style={{ flex: 1 }}>
                  {o.label}
                </span>
                {o.value === value && <Icon name="check" size={16} strokeWidth={2.6} className="mi-icon" />}
              </button>
            ))}
          </div>
          {extra?.(close)}
        </Menu>
      )}
    </>
  );
}
