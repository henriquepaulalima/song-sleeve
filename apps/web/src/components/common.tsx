import { useEffect, useId, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Link } from 'react-router';
import { LOCALES, checkPassword, type Locale } from '@song-sleeve/core';
import { useI18n, useT } from '../app/i18n';
import { Icon, type IconName } from './Icon';

export function Brand({ to, onSurface }: { to?: string; onSurface?: boolean }) {
  const inner = (
    <>
      <span aria-hidden="true" className={'brand-mark' + (onSurface ? ' on-surface' : '')} />
      <span className="brand-name">Song Sleeve</span>
    </>
  );
  return to ? (
    <Link to={to} className="brand">
      {inner}
    </Link>
  ) : (
    <div className="brand">{inner}</div>
  );
}

/** The Start screen's English / Português (Brasil) toggle. */
export function LangToggle({ value, onChange }: { value: Locale; onChange: (l: Locale) => void }) {
  const t = useT();
  return (
    <div role="group" aria-label={t('common.language')} className="lang-toggle">
      <Icon name="globe" />
      {LOCALES.map((l) => (
        <button key={l.id} type="button" lang={l.id} aria-pressed={value === l.id} onClick={() => onChange(l.id)}>
          {l.label}
        </button>
      ))}
    </div>
  );
}

export function LangSelect({ value, onChange, className }: { value: Locale; onChange: (l: Locale) => void; className?: string }) {
  const t = useT();
  const id = useId();
  return (
    <div className="select-wrap" style={{ width: 'auto' }}>
      <label htmlFor={id} className="sr-only">
        {t('common.language')}
      </label>
      <select id={id} className={className ?? 'lang-select'} value={value} onChange={(e) => onChange(e.target.value as Locale)}>
        {LOCALES.map((l) => (
          <option key={l.id} value={l.id} lang={l.id}>
            {l.label}
          </option>
        ))}
      </select>
      <Icon name="chevronDown" />
    </div>
  );
}

/** Password rules shown under the field and checked as the user types (R-AUTH-9). */
export function PasswordRules({ id, password }: { id: string; password: string }) {
  const t = useT();
  const r = checkPassword(password);
  const rule = (ok: boolean, text: string) => (
    <li className={'rule' + (ok ? ' ok' : '')}>
      <span aria-hidden="true" className="rule-dot">
        {ok && <Icon name="check" size={12} strokeWidth={3} />}
      </span>
      {text}
      <span className="sr-only">{ok ? t('auth.ruleDone') : t('auth.ruleNotYet')}</span>
    </li>
  );
  return (
    <ul id={id} aria-live="polite" className="rules">
      {rule(r.length, t('auth.ruleLength'))}
      {rule(r.notCommon, t('auth.ruleCommon'))}
    </ul>
  );
}

export function Alert({ children, icon = 'alert' }: { children: ReactNode; icon?: IconName }) {
  return (
    <p role="alert" className="alert">
      <Icon name={icon} />
      <span>{children}</span>
    </p>
  );
}

export function Note({ children, icon = 'info' }: { children: ReactNode; icon?: IconName }) {
  return (
    <p className="note">
      <Icon name={icon} />
      <span>{children}</span>
    </p>
  );
}

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

/**
 * Modal dialog: scrim, Escape to close, focus moved in and trapped, focus
 * restored on close.
 */
export function Dialog({
  title,
  onClose,
  children,
  width,
  role = 'dialog',
  hideClose,
  labelledBy,
  describedBy,
  dark,
}: {
  title?: ReactNode;
  onClose?: () => void;
  children: ReactNode;
  width?: number;
  role?: 'dialog' | 'alertdialog';
  hideClose?: boolean;
  labelledBy?: string;
  describedBy?: string;
  dark?: boolean;
}) {
  const t = useT();
  const ref = useRef<HTMLElement>(null);
  const titleId = useId();
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const el = ref.current;
    const first = el?.querySelector<HTMLElement>('[data-autofocus]') ?? el?.querySelector<HTMLElement>(FOCUSABLE);
    (first ?? el)?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && onClose) {
        e.stopPropagation();
        onClose();
      }
      if (e.key === 'Tab' && el) {
        const items = [...el.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((x) => x.offsetParent !== null);
        if (items.length === 0) return;
        const firstEl = items[0]!;
        const lastEl = items[items.length - 1]!;
        if (e.shiftKey && document.activeElement === firstEl) {
          e.preventDefault();
          lastEl.focus();
        } else if (!e.shiftKey && document.activeElement === lastEl) {
          e.preventDefault();
          firstEl.focus();
        }
      }
    };
    document.addEventListener('keydown', onKey);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = overflow;
      prev?.focus?.();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return createPortal(
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose?.()}>
      <section
        ref={ref}
        role={role}
        aria-modal="true"
        aria-labelledby={labelledBy ?? (title ? titleId : undefined)}
        aria-describedby={describedBy}
        tabIndex={-1}
        className="dialog"
        style={{ ...(width ? { width } : null), ...(dark ? { background: 'var(--ink)' } : null) }}
      >
        {onClose && !hideClose && (
          <button type="button" className="dialog-x" aria-label={t('common.close')} onClick={onClose}>
            <Icon name="close" />
          </button>
        )}
        {title && (
          <h1 id={titleId} className="dialog-title">
            {title}
          </h1>
        )}
        {children}
      </section>
    </div>,
    document.body,
  );
}

export function SearchField({ label, placeholder, value, onChange, maxWidth = 420 }: { label: string; placeholder: string; value: string; onChange: (v: string) => void; maxWidth?: number }) {
  const id = useId();
  return (
    <div className="search" style={{ maxWidth }}>
      <Icon name="search" />
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <input id={id} type="search" placeholder={placeholder} value={value} onChange={(e) => onChange(e.target.value)} />
    </div>
  );
}

export function Spinner({ label }: { label?: string }) {
  return <span role={label ? 'status' : undefined} aria-label={label} className="spinner" />;
}

export function EmptyState({ icon, title, body, action }: { icon?: IconName; title: string; body?: string; action?: ReactNode }) {
  return (
    <div className="state-mid">
      {icon && (
        <span aria-hidden="true" className="state-icon">
          <Icon name={icon} size={30} />
        </span>
      )}
      <p className="ttl">{title}</p>
      {body && <p className="sub">{body}</p>}
      {action}
    </div>
  );
}

export function SkeletonRows({ rows = 4 }: { rows?: number }) {
  const t = useT();
  return (
    <div aria-hidden="false" role="status" aria-label={t('state.loadingLibrary')} style={{ display: 'flex', flexDirection: 'column', gap: 10, marginTop: 18 }}>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="skeleton" style={{ height: 56, opacity: 1 - i * 0.18 }} />
      ))}
    </div>
  );
}

export function useLocaleState(): [Locale, (l: Locale) => void] {
  const { locale, setLocale } = useI18n();
  return [locale, setLocale];
}
