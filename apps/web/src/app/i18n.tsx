import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { detectLocale, translate, type Locale, type MessageKey, type Params } from '@song-sleeve/core';

interface I18n {
  locale: Locale;
  setLocale: (l: Locale) => void;
  t: (key: MessageKey, params?: Params) => string;
}

const Ctx = createContext<I18n | null>(null);

/**
 * The language switch before login lives in memory only (R-WEB stores nothing);
 * after login the account's language applies (D-14).
 */
export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocale] = useState<Locale>(() => detectLocale());
  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);
  const t = useCallback((key: MessageKey, params?: Params) => translate(locale, key, params), [locale]);
  const value = useMemo(() => ({ locale, setLocale, t }), [locale, t]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useI18n(): I18n {
  const v = useContext(Ctx);
  if (!v) throw new Error('I18nProvider missing');
  return v;
}

export function useT() {
  return useI18n().t;
}
