import { useId } from 'react';
import { formatBytes } from '@song-sleeve/core';
import type { DialogSpec } from '../app/dialogs';
import { useCatalog } from '../app/hooks';
import { useI18n } from '../app/i18n';
import { platform } from '../app/platform';
import { scopeStats, useSyncToggle, type ScopeSpec } from '../app/syncScope';
import { Dialog } from '../components/common';
import { Icon } from '../components/Icon';
import { SyncSwitch } from '../components/SyncControl';

type Spec = Extract<DialogSpec, { kind: 'syncScope' }>;

/**
 * Details of a sync scope (§7.4, designs/proposals/Round2 #sync): the same switch
 * as the page, then what's synced, queued and failing. Every installed device
 * queues its own local songs in an enabled scope (R-CLOUD-1); failed songs are
 * left out (D-25). The web has no local songs, so it shows progress only.
 */
export default function SyncScopeDialog({ spec, onClose }: { spec: Spec; onClose: () => void }) {
  const { t, locale } = useI18n();
  const s = useCatalog();
  const switchId = useId();
  const scope: ScopeSpec = spec.scope === 'library' ? { scope: 'library' } : { scope: spec.scope, id: spec.id };
  const { enabled, busy, cloud, toggle } = useSyncToggle(scope);
  const on = enabled && cloud;
  const web = platform.kind === 'web';
  const st = scopeStats(s, scope);
  const name = spec.scope === 'library' ? t('scope.libraryTitle') : spec.scope === 'playlist' ? (s.playlists.get(spec.id)?.name ?? '') : (s.groups.get(spec.id)?.name ?? '');
  // Before it's on, the middle figure is what turning it on would upload.
  const showUpload = !on && !web;
  const pending = st.synced + st.inQueue + st.toUpload;
  const done = pending ? st.synced / pending : 0;

  return (
    <Dialog title={t('scope.dialogTitle', { name })} onClose={onClose} width={560}>
      <div className="switch-row">
        <label htmlFor={switchId}>
          <strong>{on ? t('scope.switchOn') : t('scope.switchOff')}</strong>
          <span className="s">{web ? t('scope.webNote') : on ? t(`scope.onSub.${spec.scope}`) : t(`scope.body.${spec.scope}`)}</span>
        </label>
        <SyncSwitch id={switchId} checked={on} busy={busy} disabled={!cloud} label={t('sync.auto')} onToggle={() => void toggle()} />
      </div>

      <dl className="scope-stats">
        <div>
          <dt>{t('scope.alreadySynced')}</dt>
          <dd>{st.synced}</dd>
        </div>
        <div>
          <dt>{showUpload ? t('scope.toUpload', { size: formatBytes(locale, st.uploadBytes) }) : t('scope.inQueue')}</dt>
          <dd>{showUpload ? st.toUpload : st.inQueue + (web ? 0 : st.toUpload)}</dd>
        </div>
        <div className={st.attention ? 'warn' : undefined}>
          <dt>{t('scope.needAttention')}</dt>
          <dd>{st.attention}</dd>
        </div>
      </dl>
      {on && st.inQueue + st.toUpload > 0 && (
        <div className="scope-prog" role="progressbar" aria-label={t('scope.inQueue')} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(done * 100)}>
          <span style={{ width: `${Math.round(done * 100)}%` }} />
        </div>
      )}

      <p className="note" style={{ marginTop: 16 }}>
        <Icon name="info" />
        <span>{on ? t('scope.offNote') : `${t('scope.parallel')} ${t('scope.failedNote')}`}</span>
      </p>
      {!cloud && (
        <p className="note" style={{ marginTop: 8 }}>
          <Icon name="lock" />
          <span>{t('sync.unavailableTip')}</span>
        </p>
      )}

      <div className="dialog-actions" style={{ marginTop: 22 }}>
        <button type="button" className="btn btn-primary btn-md" onClick={onClose}>
          {t('common.close')}
        </button>
      </div>
    </Dialog>
  );
}
