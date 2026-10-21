import { createRoot } from 'react-dom/client';
import { HashRouter, Navigate, Route } from 'react-router';
import { Root } from '@web/app/Root';
import Settings from './Settings';

// WebKit's own right-click menu (Back, Reload, Inspect…) isn't part of the app.
// Songs, cards and links open their own menu; text fields keep copy and paste.
document.addEventListener('contextmenu', (e) => {
  if (!(e.target as Element).closest?.('input, textarea, [contenteditable="true"]')) e.preventDefault();
});

// The asset server serves only index.html, so routes live in the hash.
createRoot(document.getElementById('root')!).render(
  <Root Router={HashRouter} start={<Navigate to="/login" replace />} extraRoutes={<Route path="/settings" element={<Settings />} />} />,
);
