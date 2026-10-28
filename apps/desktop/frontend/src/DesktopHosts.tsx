/**
 * Desktop-only pieces that live beside the screens: the tray menu's labels and
 * commands, the startup notice about missing Linux codecs, and the notice when
 * a new version is found in the background (§9.4).
 */
import { useEffect, useRef } from 'react';
import { useNavigate } from 'react-router';
import { useIsPlaying, usePlayerValue, useSignal } from '@web/app/hooks';
import { useI18n } from '@web/app/i18n';
import { useSession } from '@web/app/session';
import { useToasts } from '@web/app/toasts';
import { local, onShell } from './bridge';
import { listFormats, missingFormats } from './codecs';
import { player, shellState } from './services';
import { openUrl, restartToUpdate, updateStatus, watchUpdates } from './updates';

function TrayBridge() {
  const { t } = useI18n();
  const playing = useIsPlaying();
  const hasTrack = usePlayerValue((s) => s.current != null);
  const title = usePlayerValue((s) => (s.current ? (s.current.artist ? `${s.current.name} · ${s.current.artist}` : s.current.name) : ''));

  useEffect(() => {
    const labels = {
      show: t('tray.show'),
      play: t('player.play'),
      pause: t('player.pause'),
      next: t('player.next'),
      previous: t('player.previous'),
      quit: t('tray.quit'),
    };
    void local('PUT', '/tray', { labels, playing, has_track: hasTrack, title }).catch(() => undefined);
  }, [t, playing, hasTrack, title]);

  useEffect(
    () =>
      onShell('tray', (action) => {
        if (action === 'toggle') void player.toggle();
        else if (action === 'next') void player.next();
        else if (action === 'previous') void player.previous();
      }),
    [],
  );
  return null;
}

function CodecNotice() {
  const { t, locale } = useI18n();
  const toasts = useToasts();
  const navigate = useNavigate();
  const { status } = useSession();
  const os = useSignal(shellState)?.os;
  const shown = useRef(false);

  useEffect(() => {
    if (shown.current || status !== 'signedIn' || os !== 'linux') return;
    const missing = missingFormats();
    shown.current = true;
    if (missing.length === 0) return;
    toasts.show(t('codecs.toast', { formats: listFormats(missing, locale) }), {
      kind: 'alert',
      ms: 10_000,
      action: { label: t('codecs.openSettings'), run: () => navigate('/settings') },
    });
  }, [status, os, t, locale, toasts, navigate]);
  return null;
}

/** Tells once per version: AppImage when it's ready to restart, .deb when it's out. */
function UpdateNotice() {
  const { t } = useI18n();
  const toasts = useToasts();
  const st = useSignal(updateStatus);
  const told = useRef('');

  useEffect(() => watchUpdates(), []);
  useEffect(() => {
    if (!st?.version || told.current === st.version) return;
    const fail = (e: unknown) => void toasts.error(e);
    if (st.state === 'ready') {
      told.current = st.version;
      toasts.show(t('update.readyToast', { v: st.version }), { ms: 15_000, action: { label: t('settings.updateRestart'), run: () => void restartToUpdate().catch(fail) } });
    } else if (st.state === 'available' && st.kind === 'deb' && st.download) {
      told.current = st.version;
      const url = st.download;
      toasts.show(t('update.availableToast'), { ms: 15_000, action: { label: t('settings.updateDeb'), run: () => void openUrl(url).catch(fail) } });
    }
  }, [st, t, toasts]);
  return null;
}

export function DesktopHosts() {
  return (
    <>
      <TrayBridge />
      <CodecNotice />
      <UpdateNotice />
    </>
  );
}
