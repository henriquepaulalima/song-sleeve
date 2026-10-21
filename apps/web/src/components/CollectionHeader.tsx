import type { ReactNode } from 'react';
import { Link, useNavigate } from 'react-router';
import { useT } from '../app/i18n';
import { Icon } from './Icon';

export interface Crumb {
  label: string;
  to?: string;
}

/**
 * Playlist and group pages (designs/proposals/Round2 #collections): back button
 * and breadcrumb, big art with the name beside it, sync and ⋯ at the end.
 * The playback controls go below, in `.coll-ctrls`.
 */
export function CollectionHeader({ crumbs, back, art, kicker, title, meta, end }: { crumbs: Crumb[]; back: string; art: ReactNode; kicker: string; title: string; meta: ReactNode; end: ReactNode }) {
  const t = useT();
  const navigate = useNavigate();
  // Back in history when the app has some, otherwise up to the parent page.
  const goBack = () => ((window.history.state as { idx?: number } | null)?.idx ? navigate(-1) : navigate(back));
  return (
    <>
      <nav aria-label={t('common.breadcrumb')} className="crumbs">
        <button type="button" className="crumb-back" aria-label={t('common.back')} data-tip={t('common.back')} onClick={goBack}>
          <Icon name="chevronLeft" size={18} />
        </button>
        {crumbs.map((c, i) => (
          <span key={i} className="crumb">
            {i > 0 && (
              <span aria-hidden="true" className="crumb-sep">
                /
              </span>
            )}
            {c.to ? <Link to={c.to}>{c.label}</Link> : <span aria-current="page">{c.label}</span>}
          </span>
        ))}
      </nav>
      <header className="coll-head">
        {art}
        <div className="coll-info">
          <div className="kicker">{kicker}</div>
          <h1 className="page-title">{title}</h1>
          <div className="coll-meta">{meta}</div>
        </div>
        <div className="coll-end">{end}</div>
      </header>
    </>
  );
}
