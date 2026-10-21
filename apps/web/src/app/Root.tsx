import { StrictMode, type ComponentType, type ReactNode } from 'react';
import { Navigate, Route, Routes, useLocation } from 'react-router';
import { useShortcuts } from './shortcuts';
import '../styles/app.css';
import { DialogProvider } from './dialogs';
import { useSignal } from './hooks';
import { I18nProvider } from './i18n';
import { updateRequired } from './services';
import { SessionProvider, useSession } from './session';
import { ToastProvider } from './toasts';
import { Spinner } from '../components/common';
import { SearchHost } from '../components/SearchPalette';
import AppShell from '../screens/AppShell';
import Artists from '../screens/Artists';
import Auth from '../screens/Auth';
import GroupPage from '../screens/Group';
import Home from '../screens/Home';
import Legal from '../screens/Legal';
import Library from '../screens/Library';
import { AcceptTerms, AccountDeleted, ComingSoon, NotFound, UpdateRequired } from '../screens/Misc';
import Playing from '../screens/Playing';
import PlaylistPage from '../screens/Playlist';
import Profile from '../screens/Profile';
import { ForgotPassword, ResetPassword } from '../screens/Recovery';
import Songs from '../screens/Songs';
import Start from '../screens/Start';

function Splash() {
  return (
    <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <Spinner />
    </div>
  );
}

/** Signed-in routes. Nothing else is reachable until new terms are accepted (R-AUTH-10). */
function RequireAuth({ children }: { children: ReactNode }) {
  const { status, me } = useSession();
  const location = useLocation();
  if (status === 'loading') return <Splash />;
  if (status === 'deleted') return <AccountDeleted />;
  if (status !== 'signedIn' || !me) return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  if (me.terms_outdated) return <AcceptTerms />;
  return (
    <>
      <ShortcutsHost />
      <SearchHost />
      {children}
    </>
  );
}

function ShortcutsHost() {
  useShortcuts();
  return null;
}

function PublicOnly({ children }: { children: ReactNode }) {
  const { status } = useSession();
  if (status === 'loading') return <Splash />;
  if (status === 'signedIn') return <Navigate to="/home" replace />;
  if (status === 'deleted') return <AccountDeleted />;
  return <>{children}</>;
}

function App({ extraRoutes, start }: { extraRoutes?: ReactNode; start?: ReactNode }) {
  const update = useSignal(updateRequired);
  return (
    <>
      <Routes>
        <Route path="/" element={<PublicOnly>{start ?? <Start />}</PublicOnly>} />
        <Route path="/login" element={<PublicOnly><Auth mode="login" /></PublicOnly>} />
        <Route path="/register" element={<PublicOnly><Auth mode="register" /></PublicOnly>} />
        <Route path="/forgot" element={<ForgotPassword />} />
        {/* Public for every account; never creates a web session (D-27). */}
        <Route path="/reset" element={<ResetPassword />} />
        <Route path="/coming-soon" element={<ComingSoon />} />
        <Route path="/legal/terms" element={<Legal doc="terms" />} />
        <Route path="/legal/privacy" element={<Legal doc="privacy" />} />
        <Route path="/playing" element={<RequireAuth><Playing /></RequireAuth>} />
        <Route element={<RequireAuth><AppShell /></RequireAuth>}>
          <Route path="/home" element={<Home />} />
          <Route path="/library" element={<Library />} />
          <Route path="/songs" element={<Songs />} />
          <Route path="/artists" element={<Artists />} />
          <Route path="/artists/:id" element={<Artists />} />
          <Route path="/playlists/:id" element={<PlaylistPage />} />
          <Route path="/groups/:id" element={<GroupPage />} />
          <Route path="/profile" element={<Profile />} />
          {extraRoutes}
          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
      {update && <UpdateRequired />}
    </>
  );
}

/** The app with its providers; the web and the desktop pick the router. */
export function Root({ Router, extraRoutes, start }: { Router: ComponentType<{ children: ReactNode }>; extraRoutes?: ReactNode; start?: ReactNode }) {
  return (
    <StrictMode>
      <Router>
        <I18nProvider>
          <ToastProvider>
            <SessionProvider>
              <DialogProvider>
                <App extraRoutes={extraRoutes} start={start} />
              </DialogProvider>
            </SessionProvider>
          </ToastProvider>
        </I18nProvider>
      </Router>
    </StrictMode>
  );
}
