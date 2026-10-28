/**
 * The app's own updates (§9.4, D-28): the shell checks the release channel, the
 * AppImage downloads and swaps itself, the .deb only links to the new package.
 */
import { local, onShell } from './bridge';
import { Signal } from '@web/app/signal';

export interface UpdateStatus {
  enabled: boolean;
  kind: '' | 'appimage' | 'deb';
  current: string;
  state: 'idle' | 'checking' | 'current' | 'available' | 'downloading' | 'ready' | 'error';
  version?: string;
  progress?: number;
  download?: string;
  error?: string;
}

export const updateStatus = new Signal<UpdateStatus | null>(null);

/** Shown when the shell can't tell where to download (development builds). */
export const RELEASES_PAGE = 'https://github.com/henriquepaulalima/song-sleeve/releases/latest';

export async function refreshUpdate(): Promise<void> {
  updateStatus.set(await local<UpdateStatus>('GET', '/update'));
}

/** Follows the shell's update events; DesktopHosts keeps this running. */
export function watchUpdates(): () => void {
  void refreshUpdate().catch(() => undefined);
  return onShell('update', (data) => {
    const prev = updateStatus.get();
    updateStatus.set({ enabled: prev?.enabled ?? true, ...(data as Omit<UpdateStatus, 'enabled'>) });
  });
}

export async function checkForUpdate(): Promise<void> {
  const st = updateStatus.get();
  if (st && !st.enabled) return;
  await local('POST', '/update/check');
}

export function restartToUpdate(): Promise<void> {
  return local('POST', '/update/restart');
}

export function openUrl(url: string): Promise<void> {
  return local('POST', '/open-url', { url });
}

/**
 * The update screen's main action: restart into a downloaded AppImage, open the
 * new .deb (or the release page), or start a check that will get there.
 */
export async function updateNow(): Promise<void> {
  const st = updateStatus.get();
  if (st?.state === 'ready') return restartToUpdate();
  if (st?.state === 'available' && st.download && (st.kind === 'deb' || st.error)) return openUrl(st.download);
  if (!st?.enabled) return openUrl(RELEASES_PAGE);
  return checkForUpdate();
}
