import { useT } from '../app/i18n';
import { SHORTCUTS } from '../app/shortcuts';

/** The keyboard shortcuts card (desktop Settings, web Profile), from the same list the handler uses. */
export function ShortcutList() {
  const t = useT();
  return (
    <section className="card" aria-labelledby="h-keys">
      <p id="h-keys" className="cap">
        {t('keys.title')}
      </p>
      <dl className="keys">
        {SHORTCUTS.map((s) => (
          <div key={s.action}>
            <dt>{t(s.label)}</dt>
            <dd>
              {s.keys.split('+').map((k, i) => (
                <span key={i}>
                  {i > 0 && '+'}
                  <kbd>{k}</kbd>
                </span>
              ))}
            </dd>
          </div>
        ))}
      </dl>
      <p style={{ margin: '12px 0 0', fontSize: 12, color: 'var(--muted-2)' }}>{t('keys.note')}</p>
    </section>
  );
}
