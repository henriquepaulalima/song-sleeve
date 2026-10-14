import { useState } from 'react';
import { Link, useNavigate } from 'react-router';
import { useT } from '../app/i18n';
import { api } from '../app/services';
import { useSession } from '../app/session';
import { useToasts } from '../app/toasts';
import { Brand, EmptyState } from '../components/common';
import { Icon, type IconName } from '../components/Icon';
import { PRIVACY_URL, TERMS_URL } from './Auth';

/** Download links per build, from GitHub Releases (D-28). Version and size are baked in by release CI. */
export const DOWNLOADS: { key: 'windows' | 'android' | 'appimage' | 'deb'; icon: IconName; href: string }[] = [
  { key: 'windows', icon: 'windows', href: 'https://github.com/henriquepaulalima/song-sleeve/releases/latest' },
  { key: 'android', icon: 'phone', href: 'https://github.com/henriquepaulalima/song-sleeve/releases/latest' },
  { key: 'appimage', icon: 'download', href: 'https://github.com/henriquepaulalima/song-sleeve/releases/latest' },
  { key: 'deb', icon: 'download', href: 'https://github.com/henriquepaulalima/song-sleeve/releases/latest' },
];

export function detectOS(): 'windows' | 'android' | 'linux' | 'other' {
  const ua = navigator.userAgent;
  if (/Android/i.test(ua)) return 'android';
  if (/Windows/i.test(ua)) return 'windows';
  if (/Linux/i.test(ua)) return 'linux';
  return 'other';
}

export function DownloadButtons({ compact }: { compact?: boolean }) {
  const t = useT();
  const os = detectOS();
  const match = (k: string) => (os === 'windows' && k === 'windows') || (os === 'android' && k === 'android') || (os === 'linux' && k === 'appimage');
  const label = { windows: t('soon.windows'), android: t('soon.android'), appimage: t('soon.appimage'), deb: t('soon.deb') };
  return (
    <div style={{ display: 'grid', gridTemplateColumns: compact ? 'repeat(2, minmax(0, 1fr))' : 'repeat(auto-fit, minmax(240px, 1fr))', gap: compact ? 8 : 10, textAlign: 'left' }}>
      {DOWNLOADS.map((d) => (
        <a key={d.key} className={'dl' + (match(d.key) ? ' primary' : '')} href={d.href} target="_blank" rel="noreferrer" style={compact ? { minHeight: 48, justifyContent: 'center' } : undefined}>
          {!compact && <Icon name={d.icon} />}
          <span style={{ display: 'flex', flexDirection: 'column' }}>
            <strong style={{ fontSize: compact ? 15 : 16 }}>{label[d.key]}</strong>
            {!compact && (
              <span style={{ fontSize: 13, fontWeight: match(d.key) ? 600 : 400, color: match(d.key) ? undefined : 'var(--muted-2)' }}>
                {match(d.key) ? t('soon.matches') : d.key === 'deb' ? t('soon.debNote') : 'GitHub Releases'}
              </span>
            )}
          </span>
        </a>
      ))}
    </div>
  );
}

/** Web coming soon (R-WEB-ACCESS): the message, the downloads and Back. */
export function ComingSoon() {
  const t = useT();
  const navigate = useNavigate();
  return (
    <div className="public">
      <div className="public-top">
        <Brand to="/" />
      </div>
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', paddingTop: 24 }}>
        <section aria-labelledby="soon-t" style={{ width: '100%', maxWidth: 640, textAlign: 'center' }}>
          <span aria-hidden="true" style={{ margin: '0 auto', width: 72, height: 72, borderRadius: 22, background: 'var(--charcoal)', color: 'var(--sand)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Icon name="globe" size={34} />
          </span>
          <h1 id="soon-t" className="page-title" style={{ margin: '22px 0 0', fontSize: 'clamp(28px, 4vw, 40px)', lineHeight: 1.15 }}>
            {t('soon.title')}
          </h1>
          <div style={{ marginTop: 30 }}>
            <DownloadButtons />
          </div>
          {detectOS() === 'windows' && <p style={{ margin: '12px 0 0', fontSize: 13, color: 'var(--muted-2)' }}>{t('soon.smartscreen')}</p>}
          <button type="button" className="btn btn-secondary" style={{ marginTop: 22 }} onClick={() => navigate(-1)}>
            <Icon name="chevronLeft" size={18} />
            {t('common.back')}
          </button>
        </section>
      </div>
    </div>
  );
}

/** Accept new terms (R-AUTH-10): nothing else is reachable until accepted. */
export function AcceptTerms() {
  const t = useT();
  const session = useSession();
  const toasts = useToasts();
  const [busy, setBusy] = useState(false);
  const accept = async () => {
    if (!session.me) return;
    setBusy(true);
    try {
      await api.post('/me/terms', { terms_version: session.me.current_terms_version });
      await session.refreshMe();
      session.retryCatalog();
    } catch (e) {
      toasts.error(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="public">
      <div className="public-top">
        <Brand />
      </div>
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <section className="panel" aria-labelledby="terms-t">
          <span className="panel-icon" aria-hidden="true">
            <Icon name="file" size={28} />
          </span>
          <h1 id="terms-t" className="panel-title">
            {t('terms.title')}
          </h1>
          <p style={{ margin: '10px 0 0', fontSize: 15, lineHeight: 1.5, color: 'var(--muted)' }}>{t('terms.body')}</p>
          <p style={{ margin: '14px 0 0', display: 'flex', gap: 14, fontSize: 15 }}>
            <a href={TERMS_URL} target="_blank" rel="noreferrer" style={{ color: 'var(--sand)', fontWeight: 600 }}>
              {t('auth.termsOfUse')}
            </a>
            <a href={PRIVACY_URL} target="_blank" rel="noreferrer" style={{ color: 'var(--sand)', fontWeight: 600 }}>
              {t('auth.privacyPolicy')}
            </a>
          </p>
          <div style={{ marginTop: 24, display: 'flex', flexDirection: 'column', gap: 8 }}>
            <button type="button" className="btn btn-primary btn-md btn-block" disabled={busy} onClick={() => void accept()}>
              {t('terms.accept')}
            </button>
            <button type="button" className="btn btn-secondary btn-md btn-block" onClick={() => void session.logout()}>
              {t('terms.logout')}
            </button>
          </div>
        </section>
      </div>
    </div>
  );
}

/** Shown for client_update_required when it persists after a reload (§8.3). */
export function UpdateRequired() {
  const t = useT();
  return (
    <div className="scrim" role="alertdialog" aria-modal="true" aria-labelledby="upd-t">
      <section className="dialog" style={{ width: 460, textAlign: 'center' }}>
        <span className="panel-icon" aria-hidden="true" style={{ margin: '0 auto' }}>
          <Icon name="retry" size={28} />
        </span>
        <h1 id="upd-t" className="dialog-title" style={{ padding: 0, marginTop: 16, fontSize: 24 }}>
          {t('update.title')}
        </h1>
        <div className="dialog-actions" style={{ justifyContent: 'center' }}>
          <button type="button" className="btn btn-primary btn-md" onClick={() => window.location.reload()}>
            {t('update.action')}
          </button>
        </div>
      </section>
    </div>
  );
}

export function AccountDeleted() {
  const t = useT();
  return (
    <div className="public">
      <div className="public-top">
        <Brand to="/" />
      </div>
      <EmptyState icon="check" title={t('del.done')} action={<Link to="/" className="btn btn-secondary">{t('common.back')}</Link>} />
    </div>
  );
}

export function NotFound() {
  const t = useT();
  return <EmptyState icon="question" title={t('state.notFound')} action={<Link to="/" className="btn btn-secondary">{t('common.back')}</Link>} />;
}
