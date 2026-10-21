import { useMemo, useSyncExternalStore } from 'react';
import type { CatalogState, PlayerState } from '@song-sleeve/core';
import { catalog, player, uploads, type Signal } from './services';
import type { UploadItem } from '../upload/uploader';

export function useCatalog(): CatalogState {
  return useSyncExternalStore(catalog.subscribe, catalog.getState);
}

/** Derives a value from the catalog, recomputed only when the catalog changes. */
export function useCatalogSelect<T>(select: (s: CatalogState) => T, deps: unknown[] = []): T {
  const s = useCatalog();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  return useMemo(() => select(s), [s, ...deps]);
}

export function usePlayer(): PlayerState {
  return useSyncExternalStore(player.subscribe, player.getState);
}

/** One primitive value from the player state (re-renders only when it changes). */
export function usePlayerValue<T extends string | number | boolean | null | undefined>(select: (s: PlayerState) => T): T {
  return useSyncExternalStore(player.subscribe, () => select(player.getState()));
}

/** The current track's id; components re-render only when it changes, not on every tick. */
export function useCurrentTrackId(): string | undefined {
  return useSyncExternalStore(player.subscribe, () => player.getState().current?.id);
}

/** True while the player is actually playing; components re-render only when it flips. */
export function useIsPlaying(): boolean {
  return useSyncExternalStore(player.subscribe, () => player.getState().status === 'playing');
}

export function useUploads(): readonly UploadItem[] {
  return useSyncExternalStore(uploads.subscribe, uploads.getItems);
}

export function useSignal<T>(s: Signal<T>): T {
  return useSyncExternalStore(s.subscribe, s.get);
}
