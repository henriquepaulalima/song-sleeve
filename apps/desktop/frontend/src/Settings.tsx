/** Settings (installed apps, §8.3; designs/WebSettings). */
import { useEffect, useState, type ReactNode } from 'react';
import { ApiError } from '@song-sleeve/core';
import { useSignal } from '@web/app/hooks';
import { useI18n } from '@web/app/i18n';
import { useToasts } from '@web/app/toasts';
import { Icon } from '@web/components/Icon';
import { ShortcutList } from '@web/components/ShortcutList';
import { local } from './bridge';
import { FORMATS, aptCommand, listFormats, missingFormats } from './codecs';
import { connection, refreshShellState, shellState } from './services';
import { checkForUpdate, openUrl, restartToUpdate, updateStatus } from './updates';

interface CheckResult {
  caught_up: boolean;
  restored: number;
  removed: number;
  need_download: number;
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4, padding: 14, borderRadius: 14, background: 'var(--well)' }}>
      <dt style={{ order: 2, fontSize: 13, color: 'var(--muted-2)' }}>{label}</dt>
      <dd className="mono" style={{ margin: 0, fontSize: 28, fontWeight: 600 }}>
        {value}
      </dd>
    </div>
  );
}

/** Missing GStreamer decoders on Linux (§9.4; designs/WebSettings). */
function CodecCard() {
  const { t, locale } = useI18n();
  const missing = missingFormats();
  const ready = FORMATS.filter((f) => !missing.includes(f));
  const n = missing.length;
  return (
    <section className="card" aria-labelledby="h-codec" style={{ boxShadow: 'inset 0 0 0 2px var(--sand)' }}>
      <p id="h-codec" className="cap">
        {t('settings.codecs')}
      </p>
      <p style={{ margin: 0, fontSize: 15, lineHeight: 1.5 }}>
        <strong>{t('settings.codecsMissing', { formats: listFormats(missing, locale), n })}</strong> {t('settings.codecsWhy', { n })}
      </p>
      <p style={{ margin: '8px 0 0', fontSize: 14, lineHeight: 1.5, color: 'var(--muted)' }}>
        {t('settings.codecsInstall', { n })}
        {ready.length > 0 && <> {t('settings.codecsReady', { formats: listFormats(ready, locale), n: ready.length })}</>}
      </p>
      <p style={{ margin: '12px 0 0', fontSize: 13, color: 'var(--muted-2)' }}>{t('settings.codecsDebian')}</p>
      <p className="mono" style={{ margin: '2px 0 0', fontSize: 12, color: 'var(--muted)', wordBreak: 'break-all', userSelect: 'text' }}>
        {aptCommand(missing)}
      </p>
    </section>
  );
}

/** Version and updates (§8.3, designs/WebSettings). */
function AboutCard({ version, dataDir }: { version?: string; dataDir?: string }) {
  const t = useI18n().t;
  const toasts = useToasts();
  const st = useSignal(updateStatus);
  const run = (f: () => Promise<void>) => () => void f().catch((e) => toasts.error(e));
  const busy = st?.state === 'checking' || st?.state === 'downloading';

  let status: ReactNode = null;
  if (st?.state === 'checking') status = t('settings.updateChecking');
  else if (st?.state === 'current') status = t('settings.updateCurrent');
  else if (st?.state === 'error') status = t('settings.updateError');
  else if (st?.state === 'downloading' && st.version) status = t('settings.updateDownloading', { v: st.version, pct: Math.round((st.progress ?? 0) * 100) });
  else if (st?.state === 'ready' && st.version)
    status = (
      <>
        <span style={{ flex: 1 }}>{t('settings.updateReady', { v: st.version })}</span>
        <button type="button" className="btn btn-primary btn-sm" onClick={run(restartToUpdate)}>
          {t('settings.updateRestart')}
        </button>
      </>
    );
  else if (st?.state === 'available' && st.download) {
    const deb = st.kind === 'deb';
    status = (
      <>
        <span style={{ flex: 1 }}>{deb ? t('settings.updateAvailable') : st.error ? t('settings.updateManual', { v: st.version ?? '' }) : t('settings.updateAvailable')}</span>
        {(deb || st.error) && (
          <button type="button" className="link-btn" style={{ color: 'inherit', fontWeight: 700 }} onClick={run(() => openUrl(st.download!))}>
            {deb ? t('settings.updateDeb') : t('settings.updatePage')}
          </button>
        )}
      </>
    );
  }

  return (
    <section className="card" aria-labelledby="h-about">
      <p id="h-about" className="cap">
        {t('settings.about')}
      </p>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
        <span style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <span style={{ fontSize: 15, fontWeight: 600 }}>Song Sleeve</span>
          <span className="mono" style={{ fontSize: 13, color: 'var(--muted-2)' }}>
            {t('settings.version', { v: version ?? '—' })}
          </span>
        </span>
        {st?.enabled ? (
          <button type="button" className="btn btn-secondary btn-md" disabled={busy} onClick={run(checkForUpdate)}>
            {t('settings.updateCheck')}
          </button>
        ) : (
          <span style={{ fontSize: 13, color: 'var(--muted-2)' }}>{t('settings.updateDev')}</span>
        )}
      </div>
      {status && (
        <p role="status" style={{ margin: '14px 0 0', display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10, padding: '12px 14px', borderRadius: 12, background: 'var(--sand)', color: 'var(--ink)', fontSize: 14 }}>
          {status}
        </p>
      )}
      <p style={{ margin: '14px 0 0', fontSize: 13, color: 'var(--muted-2)' }}>{t('settings.dataDir')}</p>
      <p className="mono" style={{ margin: '2px 0 0', fontSize: 12, color: 'var(--muted)', wordBreak: 'break-all' }}>
        {dataDir ?? '—'}
      </p>
    </section>
  );
}

export default function Settings() {
  const { t } = useI18n();
  const toasts = useToasts();
  const st = useSignal(shellState);
  const conn = useSignal(connection);
  const [checking, setChecking] = useState(false);
  const [result, setResult] = useState<CheckResult | null>(null);
  const [checkError, setCheckError] = useState<string | null>(null);
  const [health, setHealth] = useState<string | null>(null);

  useEffect(() => {
    void refreshShellState().catch(() => undefined);
  }, []);

  const check = async () => {
    setChecking(true);
    setCheckError(null);
    try {
      setResult(await local<CheckResult>('POST', '/check-folder'));
    } catch (e) {
      setResult(null);
      setCheckError(e instanceof ApiError && e.code === 'connection_required' ? t('settings.checkNeedsConnection') : toasts.error(e));
    } finally {
      setChecking(false);
    }
  };

  const checkHealth = async () => {
    try {
      const r = await local<{ changed: number }>('POST', '/health');
      setHealth(r.changed > 0 ? t('settings.healthDone', { n: r.changed }) : t('settings.healthNone'));
    } catch (e) {
      toasts.error(e);
    }
  };

  const open = () => void local('POST', '/reveal', { folder: true }).catch((e) => toasts.error(e));

  return (
    <>
      <h1 className="page-title">{t('settings.title')}</h1>
      {conn === 'offline' && (
        <p role="status" className="note" style={{ marginTop: 16 }}>
          <Icon name="wifiOff" />
          <span>{t('settings.offline')}</span>
        </p>
      )}
      <div style={{ marginTop: 24, display: 'flex', flexWrap: 'wrap', gap: 20, alignItems: 'flex-start' }}>
        <div style={{ flex: '1 1 420px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 20 }}>
          <section className="card" aria-labelledby="h-folder">
            <p id="h-folder" className="cap">
              {t('settings.folder')}
            </p>
            <p className="mono" style={{ margin: 0, fontSize: 14, wordBreak: 'break-all' }}>
              {st?.folder?.account ?? '—'}
            </p>
            <p className="alert" role="note" style={{ marginTop: 12 }}>
              <Icon name="alert" />
              <span>{t('settings.folderWarn')}</span>
            </p>
            {checking && (
              <p role="status" style={{ margin: '14px 0 0', fontSize: 14, color: 'var(--muted)' }}>
                {t('settings.checking')}
              </p>
            )}
            {result && !checking && (
              <>
                <p role="status" style={{ margin: '14px 0 0', fontSize: 14, color: 'var(--muted)' }}>
                  {t('settings.checked')}
                </p>
                <dl style={{ margin: '12px 0 0', display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 10 }}>
                  <Stat label={t('settings.restored')} value={result.restored} />
                  <Stat label={t('settings.removed')} value={result.removed} />
                  <Stat label={t('settings.needDownload')} value={result.need_download} />
                </dl>
              </>
            )}
            {checkError && (
              <p role="alert" className="note" style={{ marginTop: 12 }}>
                <Icon name="wifiOff" />
                <span>{checkError}</span>
              </p>
            )}
            <div style={{ marginTop: 16, display: 'flex', gap: 10, flexWrap: 'wrap' }}>
              <button type="button" className="btn btn-primary btn-md" disabled={checking} onClick={() => void check()}>
                <Icon name="retry" size={18} />
                {t('settings.check')}
              </button>
              <button type="button" className="btn btn-secondary btn-md" onClick={open}>
                <Icon name="folderOpen" size={18} />
                {t('settings.open')}
              </button>
            </div>
          </section>

          <section className="card" aria-labelledby="h-local">
            <p id="h-local" className="cap">
              {t('settings.local')}
            </p>
            {health && (
              <p role="status" style={{ margin: '0 0 12px', fontSize: 14, color: 'var(--muted)' }}>
                {health}
              </p>
            )}
            <button type="button" className="btn btn-secondary btn-md" onClick={() => void checkHealth()}>
              <Icon name="search" size={18} />
              {t('settings.health')}
            </button>
          </section>
        </div>

        <div style={{ flex: '1 1 320px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 20 }}>
          {st?.os === 'linux' && missingFormats().length > 0 && <CodecCard />}
          <section className="card" aria-labelledby="h-privacy">
            <p id="h-privacy" className="cap">
              {t('settings.privacy')}
            </p>
            <p style={{ margin: 0, display: 'flex', gap: 8, fontSize: 14, lineHeight: 1.45 }}>
              <Icon name="lock" size={18} style={{ flex: 'none', color: 'var(--sand)' }} />
              {st?.keyring === false ? t('settings.noKeyring') : t('settings.keyring')}
            </p>
          </section>
          <ShortcutList />
          <AboutCard version={st?.version} dataDir={st?.data_dir} />
        </div>
      </div>
    </>
  );
}
