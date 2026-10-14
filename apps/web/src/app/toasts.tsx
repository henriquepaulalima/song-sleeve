import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react';
import { ApiError, ConnectionRequiredError, UnknownResultError, type MessageKey } from '@song-sleeve/core';
import { Icon, type IconName } from '../components/Icon';
import { useT } from './i18n';

interface Toast {
  id: number;
  text: string;
  kind: 'info' | 'alert';
  icon: IconName;
  action?: { label: string; run: () => void };
}

interface Toasts {
  show: (text: string, opts?: { kind?: Toast['kind']; icon?: IconName; action?: Toast['action']; ms?: number }) => void;
  /** Shows the right message for a failed change (R-MUT-1, R-MUT-4) and returns it. */
  error: (e: unknown) => string;
}

const Ctx = createContext<Toasts | null>(null);

const known: Partial<Record<string, MessageKey>> = {
  invalid_credentials: 'error.invalid_credentials',
  rate_limited: 'error.rate_limited',
  password_invalid: 'error.password_invalid',
  password_too_common: 'error.password_too_common',
  terms_required: 'error.terms_required',
  account_suspended: 'error.account_suspended',
  invalid_field: 'error.invalid_field',
  revision_conflict: 'error.revision_conflict',
  not_found: 'error.not_found',
  limit_reached: 'error.limit_reached',
  artist_in_use: 'error.artist_in_use',
  storage_quota_exceeded: 'error.storage_quota_exceeded',
  cloud_sync_unavailable: 'error.cloud_sync_unavailable',
  operation_not_active: 'error.operation_not_active',
  reauthentication_required: 'error.reauthentication_required',
  connection_required: 'state.connectionRequired',
};

/** Maps any error to a user message in the current language. */
export function useErrorText() {
  const t = useT();
  return useCallback(
    (e: unknown): string => {
      if (e instanceof UnknownResultError) return t('state.unknownResult');
      if (e instanceof ConnectionRequiredError) return t('state.connectionRequired');
      if (e instanceof ApiError) {
        if (e.code === 'invalid_field' && e.details.reason === 'artist_exists') return t('coll.artistExists');
        const key = known[e.code];
        if (key) return t(key);
      }
      return t('error.generic');
    },
    [t],
  );
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const seq = useRef(0);
  const errorText = useErrorText();

  const dismiss = useCallback((id: number) => setToasts((ts) => ts.filter((x) => x.id !== id)), []);

  const show = useCallback<Toasts['show']>(
    (text, opts = {}) => {
      const id = ++seq.current;
      setToasts((ts) => [...ts.filter((x) => x.text !== text).slice(-3), { id, text, kind: opts.kind ?? 'info', icon: opts.icon ?? (opts.kind === 'alert' ? 'alert' : 'info'), action: opts.action }]);
      setTimeout(() => dismiss(id), opts.ms ?? 6000);
    },
    [dismiss],
  );

  const error = useCallback(
    (e: unknown) => {
      const text = errorText(e);
      const icon: IconName = e instanceof UnknownResultError ? 'question' : 'alert';
      show(text, { kind: e instanceof ConnectionRequiredError || e instanceof UnknownResultError ? 'info' : 'alert', icon });
      return text;
    },
    [errorText, show],
  );

  const value = useMemo(() => ({ show, error }), [show, error]);
  return (
    <Ctx.Provider value={value}>
      {children}
      <div className="toasts" aria-live="polite">
        {toasts.map((x) => (
          <div key={x.id} className={'toast' + (x.kind === 'alert' ? ' alert-kind' : '')} role={x.kind === 'alert' ? 'alert' : 'status'}>
            <Icon name={x.icon} />
            <span>{x.text}</span>
            {x.action && (
              <button type="button" className="link-btn" onClick={() => (x.action!.run(), dismiss(x.id))}>
                {x.action.label}
              </button>
            )}
          </div>
        ))}
      </div>
    </Ctx.Provider>
  );
}

export function useToasts(): Toasts {
  const v = useContext(Ctx);
  if (!v) throw new Error('ToastProvider missing');
  return v;
}
