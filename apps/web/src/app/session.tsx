import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { ApiError, type AuthResponse, type Locale, type Me } from '@song-sleeve/core';
import { useI18n } from './i18n';
import { useSignal } from './hooks';
import { api, catchUp, connection, resetAll, startSync, unauthenticated } from './services';
import { useToasts } from './toasts';

type Status = 'loading' | 'signedOut' | 'signedIn' | 'deleted';

interface Session {
  status: Status;
  me: Me | null;
  /** Catalog loading state for the shell (skeletons while the snapshot loads). */
  catalogError: unknown;
  signedIn: (r: AuthResponse) => void;
  refreshMe: () => Promise<void>;
  logout: () => Promise<void>;
  /** Clears local state after a server-side logout (revoke-all, account deleted). */
  dropSession: (status?: Status) => void;
  retryCatalog: () => void;
  setLocale: (l: Locale) => Promise<void>;
}

const Ctx = createContext<Session | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Status>('loading');
  const [me, setMe] = useState<Me | null>(null);
  const [catalogError, setCatalogError] = useState<unknown>(null);
  const { setLocale: setUiLocale, t } = useI18n();
  const toasts = useToasts();
  const meRef = useRef<Me | null>(null);
  meRef.current = me;

  const applyMe = useCallback(
    (m: Me) => {
      setMe(m);
      api.setCsrfToken(m.csrf_token);
      setUiLocale(m.locale);
    },
    [setUiLocale],
  );

  const dropSession = useCallback((next: Status = 'signedOut') => {
    resetAll();
    setMe(null);
    setStatus(next);
  }, []);

  const refreshMe = useCallback(async () => {
    const m = await api.get<Me>('/me');
    applyMe(m);
  }, [applyMe]);

  const boot = useCallback(
    async (m: Me) => {
      applyMe(m);
      setStatus('signedIn');
      if (m.terms_outdated) return;
      setCatalogError(null);
      try {
        await startSync(m.device_id, {
          onAccountChanged: () => void refreshMe().catch(() => undefined),
          onAccountDeleted: () => dropSession('deleted'),
        });
      } catch (e) {
        setCatalogError(e);
      }
    },
    [applyMe, dropSession, refreshMe],
  );

  // Resume an existing cookie session after a reload; everything else is refetched.
  useEffect(() => {
    let cancelled = false;
    api
      .get<Me>('/me')
      .then((m) => !cancelled && void boot(m))
      .catch(() => !cancelled && setStatus('signedOut'));
    return () => {
      cancelled = true;
    };
  }, [boot]);

  // A 401 anywhere returns the web to Login (§8.7).
  const unauth = useSignal(unauthenticated);
  useEffect(() => {
    if (unauth > 0 && meRef.current) dropSession();
  }, [unauth, dropSession]);

  // Recover changes whose result was unknown once the stream is live again (R-MUT-4).
  const conn = useSignal(connection);
  useEffect(() => {
    if (conn !== 'live' || api.outstanding.size === 0) return;
    for (const { requestId } of [...api.outstanding.values()]) {
      api
        .mutationStatus(requestId)
        .then((s) => {
          toasts.show(t(s.status === 'committed' ? 'state.recoveredApplied' : 'state.recoveredNotApplied'));
          if (s.status === 'committed') void catchUp();
        })
        .catch(() => undefined);
    }
  }, [conn, t, toasts]);

  const signedIn = useCallback((r: AuthResponse) => void boot(r.account), [boot]);

  const logout = useCallback(async () => {
    try {
      await api.post('/auth/logout');
    } catch (e) {
      if (!(e instanceof ApiError)) throw e;
    }
    dropSession();
  }, [dropSession]);

  const retryCatalog = useCallback(() => {
    if (meRef.current) void boot(meRef.current);
  }, [boot]);

  const setLocale = useCallback(
    async (l: Locale) => {
      const cur = meRef.current;
      if (!cur) return setUiLocale(l);
      const m = await api.mutate<Me>('PATCH', '/me', { locale: l, expected_revision: cur.revision });
      applyMe({ ...m, csrf_token: m.csrf_token ?? cur.csrf_token });
    },
    [applyMe, setUiLocale],
  );

  const value = useMemo<Session>(
    () => ({ status, me, catalogError, signedIn, refreshMe, logout, dropSession, retryCatalog, setLocale }),
    [status, me, catalogError, signedIn, refreshMe, logout, dropSession, retryCatalog, setLocale],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useSession(): Session {
  const v = useContext(Ctx);
  if (!v) throw new Error('SessionProvider missing');
  return v;
}
