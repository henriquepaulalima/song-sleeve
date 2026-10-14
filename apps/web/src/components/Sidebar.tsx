import { NavLink, useLocation } from 'react-router';
import { useCatalogSelect } from '../app/hooks';
import { useT } from '../app/i18n';
import { useSession } from '../app/session';
import { Brand } from './common';
import { Icon, type IconName } from './Icon';
import { newestFirst } from '@song-sleeve/core';

/** designs/WebSidebar. The web has no Settings screen (§8.3). */
export function Sidebar() {
  const t = useT();
  const { me } = useSession();
  const { pathname } = useLocation();
  const playlists = useCatalogSelect((s) => newestFirst(s.playlists.values()));
  const groups = useCatalogSelect((s) => newestFirst(s.groups.values()));
  const inCollection = pathname.startsWith('/playlists/') || pathname.startsWith('/groups/');
  const items: [string, IconName, string][] = [
    ['/home', 'home', t('nav.home')],
    ['/library', 'library', t('nav.library')],
    ['/songs', 'songs', t('nav.songs')],
    ['/artists', 'artist', t('nav.artists')],
    ['/profile', 'profile', t('nav.profile')],
  ];
  return (
    <aside aria-label={t('nav.sidebar')} className="sidebar">
      <Brand to="/home" onSurface />
      <nav aria-label={t('nav.main')} className="nav">
        {items.map(([to, icon, label]) => (
          <NavLink key={to} to={to} aria-current={(to === '/library' && inCollection) || pathname.startsWith(to) ? 'page' : undefined}>
            <Icon name={icon} />
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="side-lists">
        {playlists.length > 0 && (
          <>
            <p className="side-h">{t('nav.playlists')}</p>
            <div style={{ display: 'flex', flexDirection: 'column' }}>
              {playlists.map((p) => (
                <NavLink key={p.id} to={`/playlists/${p.id}`} className="side-l" aria-current={pathname === `/playlists/${p.id}` ? 'page' : undefined}>
                  <Icon name="songs" />
                  <span className="ellipsis">{p.name}</span>
                </NavLink>
              ))}
            </div>
          </>
        )}
        {groups.length > 0 && (
          <>
            <p className="side-h">{t('nav.groups')}</p>
            <div style={{ display: 'flex', flexDirection: 'column' }}>
              {groups.map((g) => (
                <NavLink key={g.id} to={`/groups/${g.id}`} className="side-l" aria-current={pathname === `/groups/${g.id}` ? 'page' : undefined}>
                  <Icon name="group" />
                  <span className="ellipsis">{g.name}</span>
                </NavLink>
              ))}
            </div>
          </>
        )}
      </div>
      {me && (
        <NavLink to="/profile" className="side-acct">
          <span aria-hidden="true" className="avatar">
            {me.email.charAt(0).toUpperCase()}
          </span>
          <span className="ellipsis" style={{ fontSize: 13, color: 'var(--muted)' }}>
            {me.email}
          </span>
        </NavLink>
      )}
    </aside>
  );
}
