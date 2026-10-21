import { useDialogs } from '../app/dialogs';
import { useCatalog } from '../app/hooks';
import { useT } from '../app/i18n';
import { scopeStats, useSyncToggle, type ScopeSpec } from '../app/syncScope';
import { Icon } from './Icon';

/** The on/off switch for a sync scope (designs/proposals/Round2 #sync). */
export function SyncSwitch({ checked, busy, disabled, label, onToggle, id }: { checked: boolean; busy?: boolean; disabled?: boolean; label: string; onToggle: () => void; id?: string }) {
  return (
    <button
      id={id}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      aria-busy={busy || undefined}
      className={'switch' + (busy ? ' busy' : '')}
      disabled={disabled}
      onClick={() => !busy && onToggle()}
    >
      <span />
    </button>
  );
}

/**
 * Automatic sync for the page's scope: a switch that applies at once and a
 * details button that opens the dialog. Songs, Artists and Library use the
 * library scope; playlist and group pages their own.
 */
export function SyncControl({ spec }: { spec: ScopeSpec }) {
  const t = useT();
  const dialogs = useDialogs();
  const s = useCatalog();
  const openDetails = () => dialogs.open({ kind: 'syncScope', ...spec });
  const { enabled, busy, cloud, toggle } = useSyncToggle(spec, { onDetails: openDetails });
  const pending = enabled ? scopeStats(s, spec).inQueue : 0;
  return (
    <div className={'sync-ctl' + (cloud ? '' : ' unavailable')} data-tip={cloud ? undefined : t('sync.unavailableTip')}>
      <span className="sync-lbl" aria-hidden="true">
        <Icon name={cloud ? (enabled ? 'cloudCheck' : 'cloudUp') : 'lock'} size={18} />
        {t('sync.auto')}
      </span>
      <SyncSwitch checked={enabled && cloud} busy={busy} disabled={!cloud} label={t('sync.auto')} onToggle={() => void toggle()} />
      <button type="button" className="sync-det" aria-label={pending ? `${t('sync.details')} · ${t('scope.inQueueN', { n: pending })}` : t('sync.details')} data-tip={t('sync.details')} data-tip-align="end" onClick={openDetails}>
        <Icon name="info" size={20} />
        {pending > 0 && (
          <span aria-hidden="true" className="sync-badge">
            {pending > 99 ? '99+' : pending}
          </span>
        )}
      </button>
    </div>
  );
}
