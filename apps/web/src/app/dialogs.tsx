import { createContext, lazy, Suspense, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';

export type DialogSpec =
  /** Opened from a playlist or artist page: that playlist is checked, that artist filled in. */
  | { kind: 'addSongs'; playlistIds?: string[]; artistId?: string }
  | { kind: 'editSong'; trackId: string }
  | { kind: 'replaceAudio'; trackId: string }
  | { kind: 'deleteSong'; trackId: string }
  /** With addTrackId, the new playlist gets that song and the user stays where they are. */
  | { kind: 'createPlaylist'; addTrackId?: string }
  | { kind: 'createGroup' }
  | { kind: 'renamePlaylist'; id: string }
  | { kind: 'renameGroup'; id: string }
  | { kind: 'renameArtist'; id: string }
  | { kind: 'addSongsToPlaylist'; playlistId: string }
  | { kind: 'addPlaylistsToGroup'; groupId: string }
  | { kind: 'addToGroups'; playlistId: string }
  | { kind: 'deletePlaylist'; id: string; fromGroupId?: string }
  | { kind: 'deleteGroup'; id: string }
  | { kind: 'deleteArtist'; id: string }
  | { kind: 'changePassword' }
  | { kind: 'deleteAccount' }
  | { kind: 'installApp' }
  /** Sync a library, playlist or group to the cloud (installed apps). */
  | { kind: 'syncScope'; scope: 'library' }
  | { kind: 'syncScope'; scope: 'playlist' | 'group'; id: string };

interface Dialogs {
  open: (d: DialogSpec) => void;
  close: () => void;
}

const Ctx = createContext<Dialogs | null>(null);

const AddSongsDialog = lazy(() => import('../dialogs/AddSongsDialog'));
const EditSongDialog = lazy(() => import('../dialogs/EditSongDialog'));
const ReplaceAudioDialog = lazy(() => import('../dialogs/ReplaceAudioDialog'));
const DeleteSongDialog = lazy(() => import('../dialogs/DeleteSongDialog'));
const NameDialog = lazy(() => import('../dialogs/NameDialog'));
const PickerDialog = lazy(() => import('../dialogs/PickerDialog'));
const DeleteCollectionDialog = lazy(() => import('../dialogs/DeleteCollectionDialog'));
const ChangePasswordDialog = lazy(() => import('../dialogs/ChangePasswordDialog'));
const DeleteAccountDialog = lazy(() => import('../dialogs/DeleteAccountDialog'));
const InstallAppDialog = lazy(() => import('../dialogs/InstallAppDialog'));
const SyncScopeDialog = lazy(() => import('../dialogs/SyncScopeDialog'));

function render(d: DialogSpec, close: () => void): ReactNode {
  switch (d.kind) {
    case 'addSongs':
      return <AddSongsDialog playlistIds={d.playlistIds} artistId={d.artistId} onClose={close} />;
    case 'editSong':
      return <EditSongDialog trackId={d.trackId} onClose={close} />;
    case 'replaceAudio':
      return <ReplaceAudioDialog trackId={d.trackId} onClose={close} />;
    case 'deleteSong':
      return <DeleteSongDialog trackId={d.trackId} onClose={close} />;
    case 'createPlaylist':
    case 'createGroup':
    case 'renamePlaylist':
    case 'renameGroup':
    case 'renameArtist':
      return <NameDialog spec={d} onClose={close} />;
    case 'addSongsToPlaylist':
    case 'addPlaylistsToGroup':
    case 'addToGroups':
      return <PickerDialog spec={d} onClose={close} />;
    case 'deletePlaylist':
    case 'deleteGroup':
    case 'deleteArtist':
      return <DeleteCollectionDialog spec={d} onClose={close} />;
    case 'changePassword':
      return <ChangePasswordDialog onClose={close} />;
    case 'deleteAccount':
      return <DeleteAccountDialog onClose={close} />;
    case 'installApp':
      return <InstallAppDialog onClose={close} />;
    case 'syncScope':
      return <SyncScopeDialog spec={d} onClose={close} />;
  }
}

export function DialogProvider({ children }: { children: ReactNode }) {
  const [current, setCurrent] = useState<DialogSpec | null>(null);
  const close = useCallback(() => setCurrent(null), []);
  const value = useMemo(() => ({ open: setCurrent, close }), [close]);
  return (
    <Ctx.Provider value={value}>
      {children}
      <Suspense fallback={null}>{current && render(current, close)}</Suspense>
    </Ctx.Provider>
  );
}

export function useDialogs(): Dialogs {
  const v = useContext(Ctx);
  if (!v) throw new Error('DialogProvider missing');
  return v;
}
