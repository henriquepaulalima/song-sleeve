import type { ReactNode } from 'react';
import { useDialogs } from '../app/dialogs';
import { useT } from '../app/i18n';
import { Icon } from './Icon';

/**
 * The header shared by Songs, Artists and Library (designs/proposals/Round2
 * #headers): title and subtitle, then the page's actions at the end.
 */
export function PageHeader({ title, sub, actions }: { title: string; sub: ReactNode; actions?: ReactNode }) {
  return (
    <header className="page-head">
      <div style={{ minWidth: 0 }}>
        <h1 className="page-title">{title}</h1>
        <p className="page-sub">{sub}</p>
      </div>
      {actions && <div className="page-acts">{actions}</div>}
    </header>
  );
}

/** "Add songs", optionally with a playlist checked or an artist filled in. */
export function AddSongsButton({ playlistIds, artistId }: { playlistIds?: string[]; artistId?: string }) {
  const t = useT();
  const dialogs = useDialogs();
  return (
    <button type="button" className="btn btn-primary" onClick={() => dialogs.open({ kind: 'addSongs', playlistIds, artistId })}>
      <Icon name="plus" size={18} strokeWidth={2.4} />
      {t('home.addSongs')}
    </button>
  );
}
