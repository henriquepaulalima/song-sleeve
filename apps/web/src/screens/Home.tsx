import { Link } from 'react-router';
import { formatLength, groupPlaylists, libraryOrder, newestFirst, playlistTracks, totalDurationMs } from '@song-sleeve/core';
import { useDialogs } from '../app/dialogs';
import { useCatalogSelect, useCurrentTrackId, useIsPlaying } from '../app/hooks';
import { useI18n } from '../app/i18n';
import { player } from '../app/services';
import { EmptyState } from '../components/common';
import { Icon } from '../components/Icon';
import { GroupStack, PlaylistStack, SongSleeve } from '../components/Sleeves';
import { WithContextMenu } from '../components/CollectionMenus';

/** designs/WebHome: one-row previews, newest first by creation time (D-36). */
export default function Home() {
  const { t, locale } = useI18n();
  const dialogs = useDialogs();
  const nowPlaying = useCurrentTrackId();
  const isPlaying = useIsPlaying();
  const data = useCatalogSelect((s) => {
    const songs = newestFirst(s.tracks.values()).slice(0, 12);
    const groups = newestFirst(s.groups.values()).slice(0, 8).map((g) => {
      const pls = groupPlaylists(s, g.id);
      const songCount = new Set(pls.flatMap((p) => p.items.map((i) => i.track_id))).size;
      return { g, meta: `${t('count.playlists', { n: pls.length })} · ${t('count.songs', { n: songCount })}` };
    });
    const playlists = newestFirst(s.playlists.values()).slice(0, 8).map((p) => {
      const tracks = playlistTracks(s, p.id);
      return { p, count: tracks.length, meta: `${t('count.songs', { n: tracks.length })} · ${formatLength(locale, totalDurationMs(tracks))}` };
    });
    const artists = s.artists;
    const order = libraryOrder(s).map((x) => x.id);
    return { songs, groups, playlists, artists, order, total: s.tracks.size };
  }, [t, locale]);

  const addBtn = (
    <button type="button" className="btn btn-primary" onClick={() => dialogs.open({ kind: 'addSongs' })}>
      <Icon name="plus" size={18} strokeWidth={2.4} />
      {t('home.addSongs')}
    </button>
  );

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 36 }}>
      <header className="page-head">
        <div>
          <h1 className="page-title">{t('home.title')}</h1>
          <p className="page-sub">{t('home.subtitle')}</p>
        </div>
        {addBtn}
      </header>

      {data.total === 0 ? (
        <EmptyState icon="note" title={t('state.noSongs')} body={t('state.noSongsBody')} action={addBtn} />
      ) : (
        <section aria-labelledby="h-songs">
          <SectionHead id="h-songs" title={t('home.songs')} note={t('home.newestFirst')} to="/library?show=songs" seeAllLabel={t('home.seeAllSongs')} />
          <div className="row1" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(150px, 1fr))' }}>
            {data.songs.map((s) => {
              const artist = s.artist_id ? data.artists.get(s.artist_id)?.name : null;
              const play = () => void player.playList(data.order, s.id, { kind: 'home', label: t('nav.home') });
              return (
                <WithContextMenu key={s.id} item={{ kind: 'song', track: s, onPlay: play }}>
                {(onContextMenu) => (
                <button
                  type="button"
                  onContextMenu={onContextMenu}
                  onClick={() => (s.id === nowPlaying ? void player.toggle() : play())}
                  style={{ display: 'flex', flexDirection: 'column', gap: 10, color: 'var(--white)', background: 'none', border: 0, padding: 0, textAlign: 'left', cursor: 'pointer', borderRadius: 14, minWidth: 0, font: 'inherit' }}
                >
                  <SongSleeve current={s.id === nowPlaying} playing={isPlaying} />
                  <span style={{ display: 'flex', flexDirection: 'column', gap: 2, minHeight: 40, minWidth: 0 }}>
                    <span className="ellipsis" style={{ fontSize: 15, fontWeight: 600 }}>
                      {s.name}
                    </span>
                    <span className="ellipsis" style={{ fontSize: 13, color: 'var(--muted)' }}>
                      {artist ?? t('common.noArtist')}
                    </span>
                  </span>
                </button>
                )}
                </WithContextMenu>
              );
            })}
          </div>
        </section>
      )}

      <section aria-labelledby="h-groups">
        <SectionHead id="h-groups" title={t('home.groups')} note={t('home.newestFirst')} to="/library?show=groups" seeAllLabel={t('home.seeAllGroups')} />
        {data.groups.length === 0 ? (
          <p style={{ margin: 0, color: 'var(--muted-2)', fontSize: 14 }}>{t('home.noGroups')}</p>
        ) : (
          <div className="row1" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))' }}>
            {data.groups.map(({ g, meta }) => (
              <WithContextMenu key={g.id} item={{ kind: 'group', group: g }}>
              {(onContextMenu) => (
              <Link to={`/groups/${g.id}`} className="wcard" onContextMenu={onContextMenu}>
                <GroupStack />
                <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0 }}>
                  <span className="ellipsis" style={{ fontSize: 16, fontWeight: 600 }}>
                    {g.name}
                  </span>
                  <span style={{ fontSize: 13, color: 'var(--muted-2)' }}>{meta}</span>
                </span>
              </Link>
              )}
              </WithContextMenu>
            ))}
          </div>
        )}
      </section>

      <section aria-labelledby="h-pl">
        <SectionHead id="h-pl" title={t('home.playlists')} note={t('home.newestFirst')} to="/library?show=playlists" seeAllLabel={t('home.seeAllPlaylists')} />
        {data.playlists.length === 0 ? (
          <p style={{ margin: 0, color: 'var(--muted-2)', fontSize: 14 }}>{t('home.noPlaylists')}</p>
        ) : (
          <div className="row1" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))' }}>
            {data.playlists.map(({ p, meta, count }) => (
              <WithContextMenu key={p.id} item={{ kind: 'playlist', playlist: p }}>
              {(onContextMenu) => (
              <Link to={`/playlists/${p.id}`} className="wcard" onContextMenu={onContextMenu}>
                <PlaylistStack songs={count} />
                <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0 }}>
                  <span className="ellipsis" style={{ fontSize: 16, fontWeight: 600 }}>
                    {p.name}
                  </span>
                  <span style={{ fontSize: 13, color: 'var(--muted-2)' }}>{meta}</span>
                </span>
              </Link>
              )}
              </WithContextMenu>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function SectionHead({ id, title, note, to, seeAllLabel }: { id: string; title: string; note: string; to: string; seeAllLabel: string }) {
  const { t } = useI18n();
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 14 }}>
      <h2 id={id} className="h2">
        {title} <span className="h2-note">{note}</span>
      </h2>
      <Link className="pill-link" to={to} aria-label={seeAllLabel}>
        {t('common.seeAll')}
        <Icon name="chevronRight" size={16} />
      </Link>
    </div>
  );
}
