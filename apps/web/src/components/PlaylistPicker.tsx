import { useId, useRef, useState } from 'react';
import { MAX_PLAYLIST_NAME, cleanName, nameProblem, newestFirst, type Playlist } from '@song-sleeve/core';
import { useCatalog } from '../app/hooks';
import { useT } from '../app/i18n';
import { api, catalog } from '../app/services';
import { useToasts } from '../app/toasts';
import { Icon } from './Icon';
import { Menu, MenuDivider, MenuItem } from './Menu';

/**
 * "Add to playlists (optional)" in Add songs: a checkbox list, so the songs can go
 * into several playlists at once. Toggling keeps the list open. New playlist…
 * turns the field into a name input; creating the playlist checks it.
 */
export function PlaylistPicker({ id, value, onChange }: { id: string; value: string[]; onChange: (ids: string[]) => void }) {
  const t = useT();
  const toasts = useToasts();
  const cat = useCatalog();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  const nameId = useId();
  const playlists = newestFirst(cat.playlists.values());
  const problem = nameProblem(name, MAX_PLAYLIST_NAME);

  const create = async () => {
    if (problem || busy) return;
    setBusy(true);
    try {
      const r = await api.mutate<{ playlist: Playlist }>('POST', '/me/playlists', { name: cleanName(name) });
      catalog.upsertPlaylist(r.playlist);
      onChange([...value, r.playlist.id]);
      setCreating(false);
      setName('');
    } catch (e) {
      toasts.error(e);
    } finally {
      setBusy(false);
    }
  };

  if (creating) {
    return (
      <div style={{ display: 'flex', gap: 8 }}>
        <label htmlFor={nameId} className="sr-only">
          {t('coll.name')}
        </label>
        <input
          id={nameId}
          className="input compact"
          type="text"
          value={name}
          placeholder={t('coll.newPlaylistPh')}
          aria-invalid={problem === 'too_long'}
          autoFocus
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              void create();
            } else if (e.key === 'Escape') {
              e.stopPropagation();
              e.nativeEvent.stopImmediatePropagation();
              setCreating(false);
            }
          }}
        />
        <button type="button" className="btn btn-primary btn-sm" style={{ minHeight: 46 }} disabled={!!problem || busy} onClick={() => void create()}>
          {t('common.create')}
        </button>
        <button type="button" className="icon-btn" style={{ width: 46, height: 46, background: 'var(--well)' }} aria-label={t('common.cancel')} onClick={() => setCreating(false)}>
          <Icon name="close" size={16} />
        </button>
      </div>
    );
  }

  const checked = new Set(value);
  const chosen = playlists.filter((p) => checked.has(p.id));
  const summary = chosen.length === 0 ? t('add.none') : chosen.length === 1 ? chosen[0]!.name : t('add.nPlaylists', { n: chosen.length });
  const toggle = (pid: string) => onChange(checked.has(pid) ? value.filter((x) => x !== pid) : [...value, pid]);
  return (
    <>
      <button
        ref={btn}
        id={id}
        type="button"
        className="input compact select-btn"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' && !open) {
            e.preventDefault();
            setOpen(true);
          }
        }}
      >
        <span className="ellipsis">{summary}</span>
        <Icon name="chevronDown" size={16} />
      </button>
      {open && (
        <Menu anchor={btn} onClose={() => setOpen(false)} label={t('add.playlist')} width={Math.max(220, btn.current?.offsetWidth ?? 0)} align="start">
          <div style={{ maxHeight: 300, overflowY: 'auto' }}>
            {playlists.map((p) => {
              const on = checked.has(p.id);
              return (
                <button key={p.id} type="button" role="menuitemcheckbox" aria-checked={on} className="mi" onClick={() => toggle(p.id)}>
                  <span className="ellipsis" style={{ flex: 1 }}>
                    {p.name}
                  </span>
                  <span aria-hidden="true" className={'checkbox-box' + (on ? ' on' : '')}>
                    {on && <Icon name="check" size={14} strokeWidth={3} />}
                  </span>
                </button>
              );
            })}
          </div>
          {playlists.length > 0 && <MenuDivider />}
          <MenuItem
            icon="plus"
            label={t('menu.newPlaylist')}
            onSelect={() => {
              setOpen(false);
              setCreating(true);
            }}
          />
        </Menu>
      )}
    </>
  );
}
