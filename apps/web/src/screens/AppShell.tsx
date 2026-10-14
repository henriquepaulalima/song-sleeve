import { Outlet } from 'react-router';
import { useCatalog, useSignal } from '../app/hooks';
import { useT } from '../app/i18n';
import { online } from '../app/services';
import { useSession } from '../app/session';
import { EmptyState, SkeletonRows } from '../components/common';
import { Icon } from '../components/Icon';
import { PlayerBar } from '../components/PlayerBar';
import { Sidebar } from '../components/Sidebar';

/** The app shell: sidebar, page, and the persistent player bar (§8.2). */
export default function AppShell() {
  const t = useT();
  const isOnline = useSignal(online);
  const s = useCatalog();
  const { catalogError, retryCatalog } = useSession();
  return (
    <div className="shell">
      {!isOnline && (
        <div role="status" className="banner">
          <Icon name="wifiOff" />
          {t('state.offline')} · <span style={{ fontWeight: 500 }}>{t('state.offlineBody')}</span>
        </div>
      )}
      <div className="shell-body">
        <div className="shell-side">
          <Sidebar />
        </div>
        <main className="shell-main">
          <div className="content">
            {catalogError && !s.loaded ? (
              <EmptyState
                icon="alert"
                title={t('state.loadError')}
                action={
                  <button type="button" className="btn btn-secondary" onClick={retryCatalog}>
                    {t('common.tryAgain')}
                  </button>
                }
              />
            ) : !s.loaded ? (
              <>
                <div className="skeleton" style={{ width: '40%', height: 34 }} />
                <div className="skeleton" style={{ width: '25%', height: 14, marginTop: 10 }} />
                <SkeletonRows rows={5} />
              </>
            ) : (
              <Outlet />
            )}
          </div>
        </main>
      </div>
      <div className="shell-player">
        <PlayerBar />
      </div>
    </div>
  );
}
