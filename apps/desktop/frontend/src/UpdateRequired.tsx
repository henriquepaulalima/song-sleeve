/**
 * Update required on installed apps (§8.3, designs/UpdateRequired): the server
 * refused this version. Local songs keep playing and editing, so Not now closes it
 * for this session; sync and server edits stay blocked.
 */
import { useEffect, useState } from 'react';
import { useSignal } from '@web/app/hooks';
import { useT } from '@web/app/i18n';
import { Signal } from '@web/app/signal';
import { useToasts } from '@web/app/toasts';
import { Icon } from '@web/components/Icon';
import { checkForUpdate, updateNow, updateStatus } from './updates';

const dismissed = new Signal(false);

export function DesktopUpdateRequired() {
  const t = useT();
  const toasts = useToasts();
  const off = useSignal(dismissed);
  const st = useSignal(updateStatus);
  const [busy, setBusy] = useState(false);

  // Look for the new version at once; the AppImage starts downloading it.
  useEffect(() => {
    void checkForUpdate().catch(() => undefined);
  }, []);

  if (off) return null;
  const update = async () => {
    setBusy(true);
    try {
      await updateNow();
    } catch (e) {
      toasts.error(e);
    } finally {
      setBusy(false);
    }
  };
  // Every outcome says something, so Update never looks like it did nothing.
  let status: string | null = null;
  if (busy || st?.state === 'checking') status = t('settings.updateChecking');
  else if (st?.state === 'downloading' && st.version) status = t('settings.updateDownloading', { v: st.version, pct: Math.round((st.progress ?? 0) * 100) });
  else if (st?.state === 'ready' && st.version) status = t('settings.updateReady', { v: st.version });
  else if (st?.state === 'available' && st.kind === 'appimage' && st.error) status = t('settings.updateManual', { v: st.version ?? '' });
  else if (st?.state === 'available') status = t('settings.updateAvailable');
  else if (st?.state === 'current') status = t('update.newest');
  else if (st?.state === 'error') status = t('settings.updateError');
  else if (st && !st.enabled) status = t('update.manual');

  return (
    <div className="scrim" role="alertdialog" aria-modal="true" aria-labelledby="upd-t" aria-describedby="upd-b">
      <section className="dialog" style={{ width: 480 }}>
        <span className="panel-icon" aria-hidden="true">
          <Icon name="retry" size={28} />
        </span>
        <h1 id="upd-t" className="dialog-title" style={{ padding: 0, marginTop: 16, fontSize: 24 }}>
          {t('update.title')}
        </h1>
        <p id="upd-b" style={{ margin: '10px 0 0', fontSize: 15, lineHeight: 1.5, color: 'var(--muted)' }}>
          {t('update.body')}
        </p>
        <div style={{ marginTop: 20, padding: 16, borderRadius: 16, background: 'var(--well)', display: 'flex', flexDirection: 'column', gap: 10 }}>
          <h2 className="cap" style={{ margin: 0 }}>
            {t('update.until')}
          </h2>
          {(['update.canPlay', 'update.canLocal'] as const).map((k) => (
            <p key={k} style={{ margin: 0, display: 'flex', gap: 10, fontSize: 14, lineHeight: 1.45 }}>
              <Icon name="check" size={20} style={{ flex: 'none', color: 'var(--sand)' }} />
              {t(k)}
            </p>
          ))}
          <p style={{ margin: 0, display: 'flex', gap: 10, fontSize: 14, lineHeight: 1.45 }}>
            <Icon name="close" size={20} style={{ flex: 'none', color: 'var(--muted-2)' }} />
            {t('update.cannot')}
          </p>
        </div>
        {status && (
          <p role="status" style={{ margin: '14px 0 0', fontSize: 14, color: 'var(--muted)' }}>
            {status}
          </p>
        )}
        <div className="dialog-actions">
          <button type="button" className="btn btn-secondary btn-md" onClick={() => dismissed.set(true)}>
            {t('update.notNow')}
          </button>
          <button type="button" className="btn btn-primary btn-md" disabled={busy || st?.state === 'downloading' || st?.state === 'checking'} onClick={() => void update()}>
            {st?.state === 'ready' ? t('settings.updateRestart') : t('update.update')}
          </button>
        </div>
      </section>
    </div>
  );
}
