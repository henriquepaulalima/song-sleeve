import { useCallback, useEffect, useState } from 'react';
import { formatBytes, formatLastActive, type SessionDoc, type Summary } from '@song-sleeve/core';
import { useDialogs } from '../app/dialogs';
import { useCatalog } from '../app/hooks';
import { useI18n } from '../app/i18n';
import { api } from '../app/services';
import { useSession } from '../app/session';
import { useToasts } from '../app/toasts';
import { LangSelect } from '../components/common';
import { Icon, type IconName } from '../components/Icon';
import { PRIVACY_URL, TERMS_URL } from './Auth';

/** designs/WebProfile. The web has no local-song count and no "Log out and remove" (§8.3). */
export default function Profile() {
  const { t, locale } = useI18n();
  const session = useSession();
  const dialogs = useDialogs();
  const toasts = useToasts();
  const cat = useCatalog();
  const me = session.me!;
  const [summary, setSummary] = useState<Summary | null>(null);
  const [sessions, setSessions] = useState<SessionDoc[]>([]);
  const [asking, setAsking] = useState<string | null>(null);

  const load = useCallback(() => {
    api.get<Summary>('/me/summary').then(setSummary).catch(() => undefined);
    api
      .get<{ sessions: SessionDoc[] }>('/me/sessions')
      .then((r) => setSessions(r.sessions))
      .catch(() => undefined);
  }, []);
  // Storage changes as songs sync or are deleted.
  useEffect(load, [load, cat.tracks.size]);

  const revoke = async (id: string) => {
    try {
      await api.mutate('DELETE', `/me/sessions/${id}`);
      setAsking(null);
      load();
    } catch (e) {
      toasts.error(e);
    }
  };
  const revokeAll = async () => {
    try {
      await api.mutate('POST', '/me/sessions/revoke-all');
      session.dropSession();
    } catch (e) {
      toasts.error(e);
    }
  };
  const changeLocale = async (l: typeof locale) => {
    try {
      await session.setLocale(l);
    } catch (e) {
      toasts.error(e);
    }
  };

  const quota = summary?.storage_quota_bytes ?? 0;
  const used = (summary?.storage_used_bytes ?? 0) + (summary?.storage_reserved_bytes ?? 0);
  const pct = quota > 0 ? Math.min(100, (used / quota) * 100) : 0;
  const near = pct >= 90;
  const deviceIcon = (p: string): IconName => (p === 'web' ? 'globe' : p === 'android' || p === 'ios' ? 'phone' : 'laptop');

  return (
    <>
      <h1 className="page-title">{t('profile.title')}</h1>
      <div style={{ marginTop: 24, display: 'flex', flexWrap: 'wrap', gap: 20, alignItems: 'flex-start' }}>
        <div style={{ flex: '1 1 360px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 20 }}>
          <section className="card" aria-labelledby="h-acc">
            <p id="h-acc" className="cap">
              {t('profile.account')}
            </p>
            <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
              <span aria-hidden="true" className="avatar" style={{ width: 52, height: 52, fontSize: 21 }}>
                {me.email.charAt(0).toUpperCase()}
              </span>
              <div style={{ minWidth: 0 }}>
                <div className="ellipsis" style={{ fontSize: 18, fontWeight: 600 }}>
                  {me.email}
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 5, marginTop: 3, fontSize: 13, color: 'var(--muted-2)' }}>
                  <Icon name="lock" size={13} />
                  {t('profile.emailFixed')}
                </div>
              </div>
            </div>
            <div style={{ marginTop: 18, maxWidth: 320 }}>
              <span className="label" style={{ display: 'block', marginBottom: 6 }}>
                {t('common.language')}
              </span>
              <LangSelect value={locale} onChange={(l) => void changeLocale(l)} className="input on-surface compact" />
              <p style={{ margin: '6px 0 0', fontSize: 13, color: 'var(--muted-2)' }}>{t('profile.languageNote')}</p>
            </div>
          </section>

          <section className="card" aria-labelledby="h-lib">
            <p id="h-lib" className="cap">
              {t('profile.library')}
            </p>
            {me.cloud_sync_enabled ? (
              <>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 12, fontSize: 15 }}>
                  <strong style={{ fontWeight: 600 }}>{t('profile.storage')}</strong>
                  <span className="mono" style={{ fontSize: 14 }}>
                    {t('profile.storageUsed', { used: formatBytes(locale, used), quota: formatBytes(locale, quota) })}
                  </span>
                </div>
                <div aria-hidden="true" style={{ marginTop: 10, height: 10, borderRadius: 10, background: 'var(--track)', overflow: 'hidden' }}>
                  <div style={{ height: '100%', width: `${pct}%`, background: near ? 'var(--orange)' : 'var(--sand)' }} />
                </div>
                {near && (
                  <p role="status" className="alert" style={{ marginTop: 12 }}>
                    <Icon name="alert" />
                    <span>{t('profile.nearFull')}</span>
                  </p>
                )}
              </>
            ) : (
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontSize: 15 }}>
                <strong style={{ fontWeight: 600 }}>{t('profile.cloudSync')}</strong>
                <span style={{ padding: '4px 10px', borderRadius: 999, background: 'var(--well)', color: 'var(--sand)', fontSize: 12, fontWeight: 700 }}>{t('common.comingSoon')}</span>
              </div>
            )}
            <dl style={{ margin: '18px 0 0', display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 10 }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 4, padding: 14, borderRadius: 14, background: 'var(--well)' }}>
                <dt style={{ order: 2, fontSize: 13, color: 'var(--muted-2)' }}>{t('profile.syncedSongs')}</dt>
                <dd className="mono" style={{ margin: 0, fontSize: 28, fontWeight: 600 }}>
                  {summary?.synced_songs ?? '—'}
                </dd>
              </div>
            </dl>
          </section>
          <p style={{ margin: '0 4px', fontSize: 14, color: 'var(--muted)' }}>
            <a href={TERMS_URL} target="_blank" rel="noreferrer" style={{ color: 'var(--sand)' }}>
              {t('auth.termsOfUse')}
            </a>{' '}
            ·{' '}
            <a href={PRIVACY_URL} target="_blank" rel="noreferrer" style={{ color: 'var(--sand)' }}>
              {t('auth.privacyPolicy')}
            </a>
          </p>
        </div>

        <div style={{ flex: '1.3 1 440px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 20 }}>
          <section className="card" aria-labelledby="h-dev">
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
              <p id="h-dev" className="cap" style={{ margin: 0 }}>
                {t('profile.devices')}
              </p>
              <button type="button" className="btn btn-secondary btn-sm" style={{ boxShadow: 'inset 0 0 0 1.5px var(--orange)' }} onClick={() => setAsking('__all')}>
                {t('profile.logoutAll')}
              </button>
            </div>
            {asking === '__all' && (
              <div role="alert" style={{ marginTop: 14, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10, padding: 14, borderRadius: 14, background: 'var(--sand)', color: 'var(--ink)' }}>
                <span style={{ flex: '1 1 240px', fontSize: 14, lineHeight: 1.45 }}>
                  <strong>{t('profile.logoutAllTitle')}</strong> {t('profile.logoutAllBody')}
                </span>
                <button type="button" className="btn btn-sm" style={{ background: 'transparent', color: 'var(--ink)', boxShadow: 'inset 0 0 0 1.5px rgba(22,22,21,.5)' }} onClick={() => setAsking(null)}>
                  {t('common.cancel')}
                </button>
                <button type="button" className="btn btn-primary btn-sm" onClick={() => void revokeAll()}>
                  {t('profile.logoutAllConfirm')}
                </button>
              </div>
            )}
            <ul style={{ listStyle: 'none', margin: '8px 0 0', padding: 0 }}>
              {sessions.map((d, i) => (
                <li key={d.id} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 14, minHeight: 68, padding: '8px 2px', boxShadow: i === 0 ? 'none' : 'inset 0 1px 0 rgba(255,255,255,.08)' }}>
                  <span aria-hidden="true" style={{ width: 44, height: 44, borderRadius: 12, background: 'var(--well)', color: d.current ? 'var(--sand)' : 'var(--white)', display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 'none' }}>
                    <Icon name={deviceIcon(d.device.platform)} size={20} />
                  </span>
                  <span style={{ flex: '1 1 200px', display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
                    <span className="ellipsis" style={{ fontSize: 15, fontWeight: 600 }}>
                      {d.device.label}
                    </span>
                    <span style={{ fontSize: 13, color: 'var(--muted-2)' }}>{d.current ? t('time.activeNow') : formatLastActive(locale, d.last_active_at)}</span>
                  </span>
                  {d.current ? (
                    <span style={{ padding: '5px 10px', borderRadius: 999, background: 'var(--sand)', color: 'var(--ink)', fontSize: 12, fontWeight: 700 }}>{t('profile.thisDevice')}</span>
                  ) : asking === d.id ? (
                    <span style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
                      <span style={{ fontSize: 13, color: 'var(--text-soft)' }}>{t('profile.signOutDevice')}</span>
                      <button type="button" className="btn btn-secondary btn-sm" onClick={() => setAsking(null)}>
                        {t('common.cancel')}
                      </button>
                      <button type="button" className="btn btn-primary btn-sm" onClick={() => void revoke(d.id)}>
                        {t('profile.logout')}
                      </button>
                    </span>
                  ) : (
                    <button type="button" className="btn btn-secondary btn-sm" aria-label={t('profile.logoutDevice', { name: d.device.label })} onClick={() => setAsking(d.id)}>
                      {t('profile.logout')}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </section>

          <section className="card" aria-labelledby="h-sec">
            <p id="h-sec" className="cap">
              {t('profile.security')}
            </p>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <button type="button" className="btn btn-well" style={{ justifyContent: 'flex-start', minHeight: 54, fontWeight: 600 }} onClick={() => dialogs.open({ kind: 'changePassword' })}>
                <Icon name="lock" size={20} style={{ color: 'var(--sand)' }} />
                {t('profile.changePassword')}
              </button>
              <button type="button" className="btn btn-well" style={{ justifyContent: 'flex-start', minHeight: 54, fontWeight: 600 }} onClick={() => void session.logout()}>
                <Icon name="logout" size={20} style={{ color: 'var(--sand)' }} />
                {t('profile.logoutCurrent')}
              </button>
            </div>
            <button type="button" className="link-btn" style={{ marginTop: 16, minHeight: 44, padding: '0 4px', fontSize: 15, fontWeight: 600, textDecorationColor: 'var(--orange)', textUnderlineOffset: 4 }} onClick={() => dialogs.open({ kind: 'deleteAccount' })}>
              {t('profile.deleteAccount')}
            </button>
          </section>
        </div>
      </div>
    </>
  );
}
