import { useId, useState, type FormEvent } from 'react';
import { checkPassword } from '@song-sleeve/core';
import { useT } from '../app/i18n';
import { api } from '../app/services';
import { useErrorText } from '../app/toasts';
import { Alert, Dialog, PasswordRules } from '../components/common';
import { Icon } from '../components/Icon';

/** Change password (R-AUTH-8): current password required; other devices are signed out. */
export default function ChangePasswordDialog({ onClose }: { onClose: () => void }) {
  const t = useT();
  const errorText = useErrorText();
  const [cur, setCur] = useState('');
  const [pw, setPw] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const curId = useId();
  const pwId = useId();
  const cfId = useId();
  const rulesId = useId();
  const valid = checkPassword(pw).valid && pw === confirm && cur.length > 0;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!valid || busy) return;
    setBusy(true);
    setError(null);
    try {
      await api.post('/me/password', { current_password: cur, new_password: pw });
      setDone(true);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t('pw.title')} onClose={onClose} width={560}>
      <form onSubmit={submit}>
        {!done && (
          <div style={{ marginTop: 18, display: 'flex', flexDirection: 'column', gap: 14 }}>
            <div className="field" style={{ gap: 6 }}>
              <label className="label-sm" htmlFor={curId}>
                {t('pw.current')}
              </label>
              <input className="input" id={curId} type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} data-autofocus />
            </div>
            <div className="field" style={{ gap: 6 }}>
              <label className="label-sm" htmlFor={pwId}>
                {t('rec.newPassword')}
              </label>
              <input className="input" id={pwId} type="password" autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} aria-describedby={rulesId} />
              <PasswordRules id={rulesId} password={pw} />
            </div>
            <div className="field" style={{ gap: 6 }}>
              <label className="label-sm" htmlFor={cfId}>
                {t('rec.confirmPassword')}
              </label>
              <input className="input" id={cfId} type="password" autoComplete="new-password" value={confirm} aria-invalid={confirm.length > 0 && confirm !== pw} onChange={(e) => setConfirm(e.target.value)} />
              {confirm.length > 0 && confirm !== pw && <span style={{ fontSize: 13, color: 'var(--sand)' }}>{t('rec.mismatch')}</span>}
            </div>
          </div>
        )}
        {done && (
          <p role="status" className="alert" style={{ marginTop: 16, fontWeight: 600 }}>
            <Icon name="check" />
            <span>{t('pw.done')}</span>
          </p>
        )}
        {error && (
          <div style={{ marginTop: 12 }}>
            <Alert>{error}</Alert>
          </div>
        )}
        <div className="dialog-actions">
          <button type="button" className="btn btn-secondary btn-md" onClick={onClose}>
            {done ? t('common.done') : t('common.cancel')}
          </button>
          {!done && (
            <button type="submit" className="btn btn-primary btn-md" disabled={!valid || busy}>
              {t('rec.change')}
            </button>
          )}
        </div>
      </form>
    </Dialog>
  );
}
