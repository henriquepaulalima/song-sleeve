import { Signal } from './signal';

/** Whether the search box (SearchPalette) is open; any control or shortcut opens it. */
export const searchOpen = new Signal(false);

export function openSearch(): void {
  searchOpen.set(true);
}
