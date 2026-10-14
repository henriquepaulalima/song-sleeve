import { useId, useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { ApiError, checkPassword, type AuthResponse } from '@song-sleeve/core';
import { useT } from '../app/i18n';
import { api } from '../app/services';
import { useSession } from '../app/session';
import { useErrorText } from '../app/toasts';
import { Alert, Brand, LangSelect, PasswordRules, useLocaleState } from '../components/common';
import { Icon } from '../components/Icon';

export const TERMS_URL = '/legal/terms';
export const PRIVACY_URL = '/legal/privacy';

/**
 * designs/WebAuth: Log in and Register. Registration signs in at once with no
 * verification email (R-AUTH-1); a taken email says so (R-AUTH-2); the rules are
 * checked as the user types (R-AUTH-9); the terms box is required (R-AUTH-10).
 * Accounts without early access get the coming-soon page (R-WEB-ACCESS).
 */
export default function Auth({ mode }: { mode: 'login' | 'register' }) {
  const t = useT();
  const navigate = useNavigate();
  const location = useLocation();
  const session = useSession();
  const errorText = useErrorText();
  const [locale, setLocale] = useLocaleState();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [terms, setTerms] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [taken, setTaken] = useState(false);
  const emailId = useId();
  const pwId = useId();
  const rulesId = useId();
  const reg = mode === 'register';
  const rules = checkPassword(password);
  const canSubmit = reg ? !!email && rules.valid && terms : !!email && !!password;

  const switchTo = (m: 'login' | 'register') => {
    setError(null);
    setTaken(false);
    navigate(m === 'login' ? '/login' : '/register', { replace: true });
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!canSubmit || busy) return;
    setBusy(true);
    setError(null);
    setTaken(false);
    try {
      const r = reg
        ? await api.post<AuthResponse>('/auth/register', { email, password, locale, accepted_terms: terms, terms_version: await currentTerms() })
        : await api.post<AuthResponse>('/auth/login', { email, password });
      session.signedIn(r);
      const back = (location.state as { from?: string } | null)?.from;
      navigate(back && back !== '/' ? back : '/home', { replace: true });
    } catch (err) {
      if (err instanceof ApiError && err.code === 'web_app_unavailable') {
        navigate('/coming-soon');
        return;
      }
      if (err instanceof ApiError && err.code === 'email_unavailable') setTaken(true);
      else setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexWrap: 'wrap' }}>
      <aside style={{ flex: '1 1 400px', padding: '32px clamp(20px, 4vw, 56px)', background: 'var(--charcoal)', display: 'flex', flexDirection: 'column', justifyContent: 'space-between', gap: 40 }}>
        <Brand to="/" onSurface />
        <div aria-hidden="true" style={{ position: 'relative', width: 'min(360px, 100%)', aspectRatio: '38 / 30', alignSelf: 'center' }}>
          <div className="vinyl" style={{ position: 'absolute', left: '36%', top: '6%', width: '60%', aspectRatio: '1' }} />
          <div
            style={{
              position: 'absolute',
              left: 0,
              top: 0,
              width: '62%',
              aspectRatio: '1 / 1.05',
              borderRadius: 16,
              background: 'var(--graphite)',
              color: 'var(--sand)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              transform: 'rotate(-4deg)',
              boxShadow: '0 30px 50px -20px rgba(0,0,0,.85)',
            }}
          >
            <Icon name="note" strokeWidth={1.6} style={{ width: '34%', height: '34%' }} />
          </div>
        </div>
        <p style={{ margin: 0, fontSize: 15, lineHeight: 1.5, color: 'var(--muted)', maxWidth: 380 }}>{t('auth.aside')}</p>
      </aside>
      <main style={{ flex: '999 1 480px', padding: '40px clamp(20px, 5vw, 80px)', display: 'flex', flexDirection: 'column' }}>
        <div style={{ alignSelf: 'flex-end' }}>
          <LangSelect value={locale} onChange={setLocale} />
        </div>
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div style={{ width: '100%', maxWidth: 440 }}>
            <h1 className="page-title" style={{ fontSize: 36 }}>
              {reg ? t('auth.createAccount') : t('auth.welcomeBack')}
            </h1>
            <div role="tablist" aria-label={t('auth.tabs')} className="seg">
              <button type="button" role="tab" aria-selected={!reg} onClick={() => switchTo('login')}>
                {t('auth.loginButton')}
              </button>
              <button type="button" role="tab" aria-selected={reg} onClick={() => switchTo('register')}>
                {t('start.register')}
              </button>
            </div>
            <form onSubmit={submit} style={{ marginTop: 22, display: 'flex', flexDirection: 'column', gap: 16 }} noValidate>
              <div className="field">
                <label className="label" htmlFor={emailId}>
                  {t('auth.email')}
                </label>
                <input
                  className="input"
                  id={emailId}
                  type="email"
                  autoComplete="email"
                  required
                  value={email}
                  aria-invalid={taken}
                  onChange={(e) => {
                    setEmail(e.target.value);
                    setTaken(false);
                  }}
                />
                {taken && (
                  <Alert>
                    {t('auth.emailTakenPre')}{' '}
                    <button type="button" className="link-btn" onClick={() => switchTo('login')}>
                      {t('auth.emailTakenLogin')}
                    </button>{' '}
                    {t('auth.emailTakenOr')}{' '}
                    <Link to="/forgot" style={{ fontWeight: 700 }}>
                      {t('auth.emailTakenReset')}
                    </Link>
                    .
                  </Alert>
                )}
              </div>
              <div className="field">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
                  <label className="label" htmlFor={pwId}>
                    {t('auth.password')}
                  </label>
                  {!reg && (
                    <Link to="/forgot" style={{ fontSize: 14, fontWeight: 600, color: 'var(--sand)', textUnderlineOffset: 3 }}>
                      {t('auth.forgot')}
                    </Link>
                  )}
                </div>
                <input
                  className="input"
                  id={pwId}
                  type="password"
                  autoComplete={reg ? 'new-password' : 'current-password'}
                  placeholder={reg ? t('auth.passwordPhRegister') : t('auth.passwordPhLogin')}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  aria-describedby={reg ? rulesId : undefined}
                />
                {reg && <PasswordRules id={rulesId} password={password} />}
              </div>
              {reg && (
                <label className="check-row">
                  <input type="checkbox" checked={terms} onChange={(e) => setTerms(e.target.checked)} />
                  <span>
                    {t('auth.termsPrefix')}{' '}
                    <a href={TERMS_URL} target="_blank" rel="noreferrer">
                      {t('auth.termsOfUse')}
                    </a>{' '}
                    {t('auth.termsAnd')}{' '}
                    <a href={PRIVACY_URL} target="_blank" rel="noreferrer">
                      {t('auth.privacyPolicy')}
                    </a>
                  </span>
                </label>
              )}
              {error && <Alert>{error}</Alert>}
              <button type="submit" className="btn btn-primary btn-lg btn-block" disabled={!canSubmit || busy} style={{ marginTop: 8 }}>
                {reg ? t('auth.createButton') : t('auth.loginButton')}
              </button>
              <p style={{ margin: 0, textAlign: 'center', fontSize: 13, color: 'var(--muted-2)' }}>{reg ? t('auth.noVerification') : t('auth.stay90')}</p>
            </form>
          </div>
        </div>
      </main>
    </div>
  );
}

let termsVersion: string | null = null;

/** The current terms version comes from the server; a stale one is refused (R-AUTH-10). */
async function currentTerms(): Promise<string> {
  termsVersion ??= (await api.get<{ version: string }>('/terms')).version;
  return termsVersion;
}
