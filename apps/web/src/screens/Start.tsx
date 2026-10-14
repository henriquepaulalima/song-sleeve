import { Link } from 'react-router';
import { Brand, LangToggle, useLocaleState } from '../components/common';
import { Icon } from '../components/Icon';
import { useT } from '../app/i18n';

/** designs/WebStart: welcome with Log in, Register and the language switch. */
export default function Start() {
  const t = useT();
  const [locale, setLocale] = useLocaleState();
  return (
    <div className="public">
      <header className="public-top">
        <Brand />
        <LangToggle value={locale} onChange={setLocale} />
      </header>
      <main style={{ flex: 1, display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'center', gap: '48px 80px', marginTop: 32 }}>
        <div aria-hidden="true" className="hero-art">
          <div className="vinyl" style={{ position: 'absolute', left: '34%', top: '8%', width: '62%', aspectRatio: '1', boxShadow: '0 30px 50px -20px rgba(0,0,0,.8)' }} />
          <div
            style={{
              position: 'absolute',
              left: '2%',
              top: '4%',
              width: '62%',
              aspectRatio: '1 / 1.06',
              borderRadius: 18,
              background: 'var(--charcoal)',
              boxShadow: 'inset 0 1px 0 rgba(255,255,255,.08), 0 40px 60px -24px rgba(0,0,0,.85)',
              transform: 'rotate(-4deg)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <div style={{ position: 'absolute', inset: 18, borderRadius: 8, boxShadow: 'inset 0 0 0 1px rgba(255,255,255,.12)' }} />
            <div style={{ width: '34%', aspectRatio: '1', borderRadius: '50%', background: 'var(--well)', color: 'var(--sand)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <Icon name="note" style={{ width: '46%', height: '46%' }} />
            </div>
          </div>
        </div>
        <div style={{ flex: '0 1 460px', minWidth: 0 }}>
          <h1 className="display" style={{ margin: 0, fontSize: 'clamp(40px, 5vw, 60px)', lineHeight: 1 }}>
            Song Sleeve
          </h1>
          <p style={{ margin: '18px 0 0', fontSize: 19, lineHeight: 1.5, color: 'var(--text-soft)' }}>{t('start.tagline')}</p>
          <div style={{ marginTop: 32, display: 'flex', flexWrap: 'wrap', gap: 12 }}>
            <Link to="/login" className="btn btn-primary btn-lg">
              {t('start.login')}
            </Link>
            <Link to="/register" className="btn btn-secondary btn-lg" style={{ boxShadow: 'inset 0 0 0 1.5px rgba(255,255,255,.6)', fontWeight: 700 }}>
              {t('start.register')}
            </Link>
          </div>
        </div>
      </main>
    </div>
  );
}
