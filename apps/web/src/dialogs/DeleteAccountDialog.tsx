import { useId, useState, type FormEvent } from 'react';
import { ApiError, newRequestId } from '@song-sleeve/core';
import { useT } from '../app/i18n';
import { api } from '../app/services';
import { useSession } from '../app/session';
import { useErrorText } from '../app/toasts';
import { Alert, Dialog, Spinner } from '../components/common';
import { Icon } from '../components/Icon';

/**
 * Delete account (§7.11): consequences, current password (reauthentication), the
 * confirmation text, then a progress state. Accepted means irreversible; a lost
 * response is recovered by request ID, never by sending a second delete.
 */
export default function DeleteAccountDialog({ onClose }: { onClose: () => void }) {
  const t = useT();
  const errorText = useErrorText();
  const session = useSession();
  const [pw, setPw] = useState('');
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const id = useId();

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!pw || busy) return;
    setBusy(true);
    setError(null);
    const requestId = newRequestId();
    try {
      const proof = await api.post<{ proof: string }>('/auth/reauthenticate', { password: pw, purpose: 'delete_account' });
      setDeleting(true);
      await api.mutate('POST', '/me/account-deletion', { proof: proof.proof, confirmed: true }, { requestId });
      // The web has nothing stored to purge; dropping in-memory state finishes it.
      setTimeout(() => session.dropSession('deleted'), 1200);
    } catch (err) {
      if (err instanceof ApiError && err.code === 'account_deleted') {
        session.dropSession('deleted');
        return;
      }
      setDeleting(false);
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  if (deleting) {
    return (
      <Dialog role="alertdialog" labelledBy="deleting-t" width={560} dark>
        <div role="status" style={{ textAlign: 'center', padding: '6px 0' }}>
          <div style={{ display: 'flex', justifyContent: 'center' }}>
            <Spinner />
          </div>
          <h1 id="deleting-t" className="dialog-title" style={{ marginTop: 18, padding: 0, fontSize: 26 }}>
            {t('del.deleting')}
          </h1>
          <p style={{ margin: '10px 0 0', fontSize: 15, lineHeight: 1.5, color: 'var(--muted)' }}>{t('del.deletingBody')}</p>
        </div>
      </Dialog>
    );
  }

  return (
    <Dialog role="alertdialog" onClose={onClose} hideClose width={600} labelledBy="del-acc-t">
      <form onSubmit={submit}>
        <span aria-hidden="true" className="danger-icon">
          <Icon name="trash" size={26} />
        </span>
        <h1 id="del-acc-t" className="dialog-title" style={{ marginTop: 16, padding: 0, fontSize: 26 }}>
          {t('del.title')}
        </h1>
        <p style={{ margin: '12px 0 0', padding: 14, borderRadius: 14, background: 'var(--well)', fontSize: 15, lineHeight: 1.55, fontWeight: 500 }}>{t('del.body')}</p>
        <p style={{ margin: '10px 0 0', fontSize: 13, color: 'var(--muted-2)' }}>{t('del.files')}</p>
        <div className="field" style={{ marginTop: 16, gap: 6 }}>
          <label className="label-sm" htmlFor={id}>
            {t('del.password')}
          </label>
          <input className="input" id={id} type="password" autoComplete="current-password" value={pw} onChange={(e) => setPw(e.target.value)} />
        </div>
        {error && (
          <div style={{ marginTop: 12 }}>
            <Alert>{error}</Alert>
          </div>
        )}
        <div className="dialog-actions" style={{ marginTop: 22 }}>
          <button type="button" className="btn btn-secondary btn-md" onClick={onClose} data-autofocus>
            {t('del.keep')}
          </button>
          <button type="submit" className="btn btn-primary btn-md" disabled={!pw || busy}>
            {t('del.confirm')}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
