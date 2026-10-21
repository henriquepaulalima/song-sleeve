/**
 * What the platform adds to the shared screens. The web adds nothing; the desktop
 * build replaces this module with its own (apps/desktop/frontend), so screens stay
 * free of platform checks beyond "is this action available?" (§4.3).
 */
import type { MessageKey, Track } from '@song-sleeve/core';
import type { IconName } from '../components/Icon';

export type Translate = (k: MessageKey, p?: Record<string, string | number>) => string;

export interface LocalSongActions {
  /** Sync to cloud (§7.2); retry covers failed syncs. */
  sync(trackIds: string[], t: Translate): Promise<void>;
  retry(trackIds: string[], t: Translate): Promise<void>;
  cancelQueued(trackIds: string[], t: Translate): Promise<void>;
  /** Relink (§7.6) and Use changed file / Replace audio file for local songs. */
  relink(track: Track, t: Translate): Promise<void>;
  replaceSource(track: Track, t: Translate): Promise<void>;
  showInFolder(track: Track, t: Translate): Promise<void>;
}

export interface Platform {
  kind: 'web' | 'desktop';
  local?: LocalSongActions;
  /** Log out and remove from this device (§7.12). */
  logoutAndRemove?: () => Promise<void>;
  /** Extra sidebar entries, e.g. Settings in installed apps. */
  nav?: { to: string; icon: IconName; label: MessageKey }[];
}

export const platform: Platform = { kind: 'web' };
