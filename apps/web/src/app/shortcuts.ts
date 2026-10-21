/**
 * Keyboard shortcuts: one definition for the handler, the tooltips and the list
 * shown in Settings, so they can't drift apart.
 */
import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router';
import type { MessageKey } from '@song-sleeve/core';
import { openSearch } from './search';
import { player } from './services';

export type ShortcutAction = 'playPause' | 'next' | 'previous' | 'seekForward' | 'seekBack' | 'shuffle' | 'repeat' | 'fullPlayer' | 'closeFull' | 'search';

export interface Shortcut {
  action: ShortcutAction;
  /** Shown to the user. */
  keys: string;
  label: MessageKey;
}

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
const mod = isMac ? '⌘' : 'Ctrl';

export const SHORTCUTS: Shortcut[] = [
  { action: 'playPause', keys: 'Space', label: 'keys.playPause' },
  { action: 'next', keys: `${mod}+→`, label: 'player.next' },
  { action: 'previous', keys: `${mod}+←`, label: 'player.previous' },
  { action: 'seekForward', keys: '→', label: 'keys.seekForward' },
  { action: 'seekBack', keys: '←', label: 'keys.seekBack' },
  { action: 'shuffle', keys: 'S', label: 'player.shuffle' },
  { action: 'repeat', keys: 'R', label: 'player.repeat' },
  { action: 'fullPlayer', keys: 'F', label: 'keys.fullPlayer' },
  { action: 'closeFull', keys: 'Esc', label: 'keys.closeFull' },
  { action: 'search', keys: `${mod}+K`, label: 'keys.search' },
];

const SEEK_STEP = 5;

/** Tooltip text for a control: "Next (Ctrl+→)". */
export function tipFor(action: ShortcutAction, t: (k: MessageKey) => string, label?: string): string {
  const s = SHORTCUTS.find((x) => x.action === action)!;
  return `${label ?? t(s.label)} (${s.keys})`;
}

function actionFor(e: KeyboardEvent): ShortcutAction | null {
  const primary = isMac ? e.metaKey : e.ctrlKey;
  if (e.altKey) return null;
  if (primary && !e.shiftKey) {
    if (e.key === 'ArrowRight') return 'next';
    if (e.key === 'ArrowLeft') return 'previous';
    if (e.key === 'f' || e.key === 'F' || e.key === 'k' || e.key === 'K') return 'search';
    return null;
  }
  if (e.ctrlKey || e.metaKey || e.shiftKey) return null;
  switch (e.key) {
    case ' ':
      return 'playPause';
    case 'ArrowRight':
      return 'seekForward';
    case 'ArrowLeft':
      return 'seekBack';
    case 's':
    case 'S':
      return 'shuffle';
    case 'r':
    case 'R':
      return 'repeat';
    case 'f':
    case 'F':
      return 'fullPlayer';
    case 'Escape':
      return 'closeFull';
    case '/':
      return 'search';
  }
  return null;
}

/** Typing, a focused control that uses the key itself, or an open dialog/menu: leave the key alone. */
function shouldIgnore(e: KeyboardEvent, action: ShortcutAction): boolean {
  if (e.defaultPrevented || e.isComposing) return true;
  if (document.querySelector('[aria-modal="true"], .menu')) return true;
  const el = document.activeElement as HTMLElement | null;
  if (!el) return false;
  // Ctrl+K / Ctrl+F open search even while typing in a field.
  if (action === 'search' && (e.ctrlKey || e.metaKey)) return false;
  if (el.isContentEditable || el.closest('input, textarea, select, [role="combobox"], [role="slider"]')) {
    // The seek bar is a range input; Space still plays/pauses from it.
    return !(action === 'playPause' && el instanceof HTMLInputElement && el.type === 'range');
  }
  // Space on a focused button presses it; arrows on a drag handle move the row.
  if (action === 'playPause' && el.closest('button, a, [role="button"]')) return true;
  if ((action === 'seekForward' || action === 'seekBack') && el.closest('.grip, [role="menuitem"]')) return true;
  return false;
}

/** The global handler, mounted once for signed-in screens. */
export function useShortcuts(): void {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const action = actionFor(e);
      if (!action || shouldIgnore(e, action)) return;
      const s = player.getState();
      const loaded = s.status !== 'empty';
      switch (action) {
        case 'playPause':
          if (!loaded) return;
          void player.toggle();
          break;
        case 'next':
          void player.next();
          break;
        case 'previous':
          if (!loaded) return;
          void player.previous();
          break;
        case 'seekForward':
          if (!loaded) return;
          player.seek(s.positionSec + SEEK_STEP);
          break;
        case 'seekBack':
          if (!loaded) return;
          player.seek(Math.max(0, s.positionSec - SEEK_STEP));
          break;
        case 'shuffle':
          player.setShuffle(!s.shuffle);
          break;
        case 'repeat':
          player.toggleRepeat();
          break;
        case 'fullPlayer':
          if (pathname === '/playing') closeFull(navigate);
          else navigate('/playing');
          break;
        case 'closeFull':
          if (pathname !== '/playing') return;
          closeFull(navigate);
          break;
        case 'search':
          openSearch();
          break;
      }
      e.preventDefault();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [navigate, pathname]);
}

/** Leaves the full-screen player: back if there's history, otherwise Songs. */
export function closeFull(navigate: ReturnType<typeof useNavigate>): void {
  if (window.history.length > 1) navigate(-1);
  else navigate('/songs');
}
