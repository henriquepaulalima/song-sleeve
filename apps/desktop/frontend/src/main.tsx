import { createRoot } from 'react-dom/client';
import { HashRouter, Navigate, Route } from 'react-router';
import { Root } from '@web/app/Root';
import { local, type ShellState } from './bridge';
import { initCrashReports } from './crash';
import { DesktopHosts } from './DesktopHosts';
import Settings from './Settings';

// WebKit's own right-click menu (Back, Reload, Inspect…) isn't part of the app.
// Songs, cards and links open their own menu; text fields keep copy and paste.
document.addEventListener('contextmenu', (e) => {
  if (!(e.target as Element).closest?.('input, textarea, [contenteditable="true"]')) e.preventDefault();
});

void local<ShellState>('GET', '/state')
  .then((st) => initCrashReports(st.crash))
  .catch(() => undefined);

// The asset server serves only index.html, so routes live in the hash.
createRoot(document.getElementById('root')!).render(
  <Root Router={HashRouter} start={<Navigate to="/login" replace />} extraRoutes={<Route path="/settings" element={<Settings />} />} extras={<DesktopHosts />} />,
);
