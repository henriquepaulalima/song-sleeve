import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { Link } from 'react-router';
import { ApiError, checkPassword } from '@song-sleeve/core';
import { useT } from '../app/i18n';
import { api } from '../app/services';
import { useErrorText } from '../app/toasts';
import { Alert, Brand, PasswordRules, Spinner } from '../components/common';
import { Icon } from '../components/Icon';

function Frame({ children }: { children: ReactNode }) {
  return (
    <div className="public">
      <div className="public-top">
        <Brand to="/" />
      </div>
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', paddingTop: 24 }}>
        <section aria-labelledby="rec-title" className="panel">
          {children}
        </section>
      </div>
    </div>
  );
}

/** Forgot password: always the same generic response (R-AUTH-2, §7.12). */
export function ForgotPassword() {
  const t = useT();
  const errorText = useErrorText();
  const [email, setEmail] = useState('');
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const id = useId();
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!email.trim() || busy) return;
    setBusy(true);
    setError(null);
    try {
      await api.post('/auth/password/forgot', { email });
      setSent(true);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };
  if (sent) {
    return (
      <Frame>
        <span className="panel-icon" aria-hidden="true">
          <Icon name="mail" size={28} />
        </span>
        <h1 id="rec-title" className="panel-title">
          {t('rec.sentTitle')}
        </h1>
        <p role="status" style={{ margin: '8px 0 0', fontSize: 16, lineHeight: 1.5 }}>
          {t('rec.sentBody')}
        </p>
        <p style={{ margin: '10px 0 0', fontSize: 14, lineHeight: 1.5, color: 'var(--muted)' }}>{t('rec.sentNote')}</p>
        <div style={{ marginTop: 22 }}>
          <Link to="/login" className="btn btn-secondary btn-md btn-block">
            {t('rec.backToLogin')}
          </Link>
        </div>
      </Frame>
    );
  }
  return (
    <Frame>
      <span className="panel-icon" aria-hidden="true">
        <Icon name="lock" size={28} />
      </span>
      <h1 id="rec-title" className="panel-title">
        {t('rec.forgotTitle')}
      </h1>
      <p style={{ margin: '8px 0 0', fontSize: 15, lineHeight: 1.5, color: 'var(--muted)' }}>{t('rec.forgotBody')}</p>
      <form onSubmit={submit} noValidate>
        <div className="field" style={{ marginTop: 20 }}>
          <label className="label" htmlFor={id}>
            {t('auth.email')}
          </label>
          <input className="input" id={id} type="email" autoComplete="email" placeholder="you@example.com" value={email} onChange={(e) => setEmail(e.target.value)} />
        </div>
        {error && (
          <div style={{ marginTop: 12 }}>
            <Alert>{error}</Alert>
          </div>
        )}
        <div style={{ marginTop: 22, display: 'flex', flexDirection: 'column', gap: 8 }}>
          <button type="submit" className="btn btn-primary btn-md btn-block" disabled={!email.trim() || busy}>
            {t('rec.send')}
          </button>
          <Link to="/login" className="btn btn-secondary btn-md btn-block">
            {t('rec.backToLogin')}
          </Link>
        </div>
      </form>
    </Frame>
  );
}

type ResetStep = 'checking' | 'form' | 'success' | 'expired' | 'sendNew';

/**
 * The public reset page (D-27): opened from the email link, open to every account,
 * never creates a web session. The token is read from the fragment and stripped
 * from the address bar at once (§7.12 hardening).
 */
export function ResetPassword() {
  const t = useT();
  const errorText = useErrorText();
  const token = useRef<string | null>(null);
  const [step, setStep] = useState<ResetStep>('checking');
  const [email, setEmail] = useState('');
  const [pw, setPw] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [webLogin, setWebLogin] = useState(false);
  const pwId = useId();
  const cfId = useId();
  const rulesId = useId();

  useEffect(() => {
    if (token.current === null) {
      const m = /token=([A-Za-z0-9_-]+)/.exec(window.location.hash);
      token.current = m?.[1] ?? '';
      history.replaceState(null, '', window.location.pathname);
    }
    if (!token.current) {
      setStep('expired');
      return;
    }
    api
      .post<{ email: string }>('/auth/password/reset/validate', { token: token.current })
      .then((r) => {
        setEmail(r.email);
        setStep('form');
      })
      .catch(() => setStep('expired'));
  }, []);

  const valid = checkPassword(pw).valid;
  const mismatch = confirm.length > 0 && confirm !== pw;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!valid || pw !== confirm || busy) return;
    setBusy(true);
    setError(null);
    try {
      const r = await api.post<{ web_login_available: boolean }>('/auth/password/reset', { token: token.current, new_password: pw });
      setWebLogin(r.web_login_available);
      setStep('success');
    } catch (err) {
      if (err instanceof ApiError && err.code === 'reset_token_invalid') setStep('expired');
      else setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  if (step === 'checking') {
    return (
      <Frame>
        <div role="status" style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 14, padding: '30px 0', textAlign: 'center' }}>
          <Spinner />
          <h1 id="rec-title" style={{ margin: 0, fontSize: 20, fontWeight: 700 }}>
            {t('rec.checking')}
          </h1>
        </div>
      </Frame>
    );
  }
  if (step === 'expired') {
    return (
      <Frame>
        <span className="panel-icon" aria-hidden="true">
          <Icon name="clock" size={28} />
        </span>
        <h1 id="rec-title" className="panel-title" style={{ fontSize: 28 }}>
          {t('rec.expiredTitle')}
        </h1>
        <p style={{ margin: '10px 0 0', fontSize: 15, lineHeight: 1.5, color: 'var(--muted)' }}>{t('rec.expiredBody')}</p>
        <div style={{ marginTop: 22 }}>
          <Link to="/forgot" className="btn btn-primary btn-md btn-block">
            {t('rec.sendNew')}
          </Link>
        </div>
      </Frame>
    );
  }
  if (step === 'success') {
    return (
      <Frame>
        <span className="panel-icon" aria-hidden="true" style={{ background: 'var(--sand)', color: 'var(--ink)' }}>
          <Icon name="check" size={28} strokeWidth={2.6} />
        </span>
        <h1 id="rec-title" role="status" className="panel-title" style={{ fontSize: 28, lineHeight: 1.2 }}>
          {t('rec.successTitle')}
        </h1>
        <p style={{ margin: '10px 0 0', fontSize: 14, color: 'var(--muted)' }}>{t('rec.successNote')}</p>
        {webLogin && (
          <div style={{ marginTop: 22 }}>
            <Link to="/login" className="btn btn-secondary btn-md btn-block">
              {t('rec.webLogin')}
            </Link>
          </div>
        )}
      </Frame>
    );
  }
  return (
    <Frame>
      <h1 id="rec-title" className="panel-title" style={{ marginTop: 0 }}>
        {t('rec.newTitle')}
      </h1>
      <p style={{ margin: '8px 0 0', fontSize: 15, color: 'var(--muted)' }}>{t('rec.for', { email })}</p>
      <form onSubmit={submit} noValidate>
        {/* Lets password managers attach the new password to the account. */}
        <input type="email" autoComplete="username" value={email} readOnly hidden />
        <div className="field" style={{ marginTop: 20 }}>
          <label className="label" htmlFor={pwId}>
            {t('rec.newPassword')}
          </label>
          <input className="input" id={pwId} type="password" autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} aria-describedby={rulesId} />
          <PasswordRules id={rulesId} password={pw} />
        </div>
        <div className="field" style={{ marginTop: 16 }}>
          <label className="label" htmlFor={cfId}>
            {t('rec.confirmPassword')}
          </label>
          <input className="input" id={cfId} type="password" autoComplete="new-password" value={confirm} aria-invalid={mismatch} onChange={(e) => setConfirm(e.target.value)} />
          {mismatch && <span style={{ fontSize: 13, color: 'var(--sand)' }}>{t('rec.mismatch')}</span>}
        </div>
        {error && (
          <div style={{ marginTop: 12 }}>
            <Alert>{error}</Alert>
          </div>
        )}
        <div style={{ marginTop: 22 }}>
          <button type="submit" className="btn btn-primary btn-md btn-block" disabled={!valid || pw !== confirm || busy}>
            {t('rec.change')}
          </button>
        </div>
      </form>
    </Frame>
  );
}
