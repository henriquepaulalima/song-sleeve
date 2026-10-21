import { useId, useState, type ReactNode } from 'react';
import { Link, useLocation } from 'react-router';
import { useCatalogSelect } from '../app/hooks';
import { openSearch } from '../app/search';
import { useT } from '../app/i18n';
import { platform } from '../app/platform';
import { useSession } from '../app/session';
import { Brand } from './common';
import { Icon, type IconName } from './Icon';
import { artistsByName, newestFirst } from '@song-sleeve/core';
import { WithContextMenu } from './CollectionMenus';

const COLLAPSED_KEY = 'song-sleeve.sidebar.collapsed';

/**
 * Which sidebar lists are collapsed: a per-device layout preference, kept in local
 * storage by installed apps. The web stores nothing on the device (R-WEB), so there
 * it lasts until reload.
 */
const persist = platform.kind !== 'web';

function readCollapsed(): Record<string, boolean> {
  if (!persist) return {};
  try {
    return JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? '{}') as Record<string, boolean>;
  } catch {
    return {};
  }
}

function useCollapsed(): [Record<string, boolean>, (key: string) => void] {
  const [collapsed, setCollapsed] = useState(readCollapsed);
  const toggle = (key: string) =>
    setCollapsed((c) => {
      const next = { ...c, [key]: !c[key] };
      try {
        if (persist) localStorage.setItem(COLLAPSED_KEY, JSON.stringify(next));
      } catch {
        /* storage unavailable: the state lasts until reload */
      }
      return next;
    });
  return [collapsed, toggle];
}

/**
 * Artists, Playlists and Groups (designs/proposals/Round3 #sidebar): a nav row whose
 * label opens the page and whose chevron folds the list underneath.
 */
function SideGroup({ to, icon, label, current, open, onToggle, children, count }: { to: string; icon: IconName; label: string; current: boolean; open: boolean; onToggle: () => void; children: ReactNode; count: number }) {
  const t = useT();
  const id = useId();
  return (
    <div className="side-g">
      <div className="side-g-row">
        <Link to={to} className="side-g-link" aria-current={current ? 'page' : undefined}>
          <Icon name={icon} />
          {label}
        </Link>
        {count > 0 && (
          <button type="button" className="side-g-tg" aria-expanded={open} aria-controls={id} aria-label={t(open ? 'nav.hideList' : 'nav.showList', { name: label })} onClick={onToggle}>
            <Icon name="chevronDown" />
          </button>
        )}
      </div>
      {count > 0 && (
        <div id={id} className="side-g-b" hidden={!open}>
          {children}
        </div>
      )}
    </div>
  );
}

/** designs/proposals/Round3 #sidebar. Settings and the account link sit at the bottom. */
export function Sidebar() {
  const t = useT();
  const { me } = useSession();
  const { pathname, search } = useLocation();
  const show = new URLSearchParams(search).get('show');
  const playlists = useCatalogSelect((s) => newestFirst(s.playlists.values()));
  const groups = useCatalogSelect((s) => newestFirst(s.groups.values()));
  const artists = useCatalogSelect((s) => artistsByName(s));
  const [collapsed, toggle] = useCollapsed();
  const items: [string, IconName, string, boolean][] = [
    ['/home', 'home', t('nav.home'), pathname.startsWith('/home')],
    // Library is selected unless a Playlists or Groups row (or one of their items) is.
    ['/library', 'library', t('nav.library'), pathname.startsWith('/library') && show !== 'playlists' && show !== 'groups'],
    ['/songs', 'songs', t('nav.songs'), pathname.startsWith('/songs')],
  ];
  const keyHint = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘K' : 'Ctrl K';
  return (
    <aside aria-label={t('nav.sidebar')} className="sidebar">
      <Brand to="/home" onSurface />
      <button type="button" className="side-search" aria-label={`${t('search.title')} (${keyHint})`} onClick={openSearch}>
        <Icon name="search" />
        <span className="side-search-label">{t('search.trigger')}</span>
        <kbd>{keyHint}</kbd>
      </button>
      <nav aria-label={t('nav.main')} className="nav">
        {items.map(([to, icon, label, current]) => (
          <Link key={to} to={to} aria-current={current ? 'page' : undefined}>
            <Icon name={icon} />
            {label}
          </Link>
        ))}
        {/* Narrow screens have no lists; Artists stays in the bar. */}
        <Link to="/artists" className="narrow-only" aria-current={pathname.startsWith('/artists') ? 'page' : undefined}>
          <Icon name="artist" />
          {t('nav.artists')}
        </Link>
      </nav>
      <div className="side-lists">
        <SideGroup to="/artists" icon="artist" label={t('nav.artists')} current={pathname === '/artists'} open={!collapsed.artists} onToggle={() => toggle('artists')} count={artists.length}>
          {artists.map((a) => (
            <Link key={a.id} to={`/artists/${a.id}`} className="side-l" aria-current={pathname === `/artists/${a.id}` ? 'page' : undefined}>
              <span aria-hidden="true" className="side-av">
                {a.name.charAt(0).toUpperCase()}
              </span>
              <span className="ellipsis">{a.name}</span>
            </Link>
          ))}
        </SideGroup>
        <SideGroup to="/library?show=playlists" icon="playlist" label={t('nav.playlists')} current={pathname.startsWith('/library') && show === 'playlists'} open={!collapsed.playlists} onToggle={() => toggle('playlists')} count={playlists.length}>
          {playlists.map((p) => (
            <WithContextMenu key={p.id} item={{ kind: 'playlist', playlist: p }}>
              {(onContextMenu) => (
                <Link to={`/playlists/${p.id}`} className="side-l" aria-current={pathname === `/playlists/${p.id}` ? 'page' : undefined} onContextMenu={onContextMenu}>
                  <Icon name="playlist" />
                  <span className="ellipsis">{p.name}</span>
                </Link>
              )}
            </WithContextMenu>
          ))}
        </SideGroup>
        <SideGroup to="/library?show=groups" icon="group" label={t('nav.groups')} current={pathname.startsWith('/library') && show === 'groups'} open={!collapsed.groups} onToggle={() => toggle('groups')} count={groups.length}>
          {groups.map((g) => (
            <WithContextMenu key={g.id} item={{ kind: 'group', group: g }}>
              {(onContextMenu) => (
                <Link to={`/groups/${g.id}`} className="side-l" aria-current={pathname === `/groups/${g.id}` ? 'page' : undefined} onContextMenu={onContextMenu}>
                  <Icon name="group" />
                  <span className="ellipsis">{g.name}</span>
                </Link>
              )}
            </WithContextMenu>
          ))}
        </SideGroup>
      </div>
      <div className="side-foot">
        {(platform.nav ?? []).length > 0 && (
          <nav aria-label={t('nav.more')} className="nav side-foot-nav">
            {(platform.nav ?? []).map((n) => (
              <Link key={n.to} to={n.to} aria-current={pathname.startsWith(n.to) ? 'page' : undefined}>
                <Icon name={n.icon} />
                {t(n.label)}
              </Link>
            ))}
          </nav>
        )}
        {me && (
          <Link to="/profile" className="side-acct" aria-label={`${t('nav.profile')} · ${me.email}`} aria-current={pathname.startsWith('/profile') ? 'page' : undefined}>
            <span aria-hidden="true" className="avatar">
              {me.email.charAt(0).toUpperCase()}
            </span>
            <span className="ellipsis" style={{ fontSize: 13, color: 'var(--muted)' }}>
              {me.email}
            </span>
          </Link>
        )}
      </div>
    </aside>
  );
}
