import { Link, useNavigate, useParams } from 'react-router';
import { artistTracks, artistsByName, songCountByArtist } from '@song-sleeve/core';
import { useDialogs } from '../app/dialogs';
import { useCatalog, useCatalogSelect, useCurrentTrackId } from '../app/hooks';
import { useT } from '../app/i18n';
import { player } from '../app/services';
import { EmptyState } from '../components/common';
import { Icon } from '../components/Icon';
import { AddSongsButton, PageHeader } from '../components/PageHeader';
import { SyncControl } from '../components/SyncControl';
import { IncrementalList } from '../components/ReorderableList';
import { SongRow, songColumns } from '../components/SongRow';

/** designs/WebArtists: the account's artists; an artist page lists its songs in library order. */
export default function Artists() {
  const t = useT();
  const navigate = useNavigate();
  const dialogs = useDialogs();
  const { id } = useParams();
  const s = useCatalog();
  const nowPlaying = useCurrentTrackId();
  const artists = useCatalogSelect((st) => artistsByName(st));
  const counts = useCatalogSelect((st) => songCountByArtist(st));
  const selected = (id && s.artists.get(id)) || artists[0];
  const songs = useCatalogSelect((st) => (selected ? artistTracks(st, selected.id) : []), [selected?.id]);

  if (artists.length === 0) {
    return (
      <>
        <PageHeader title={t('artists.title')} sub={t('count.artists', { n: 0 })} actions={headerActions(undefined)} />
        <EmptyState icon="artist" title={t('artists.empty')} body={t('artists.emptyBody')} />
      </>
    );
  }
  const ids = songs.map((x) => x.id);
  const count = selected ? (counts.get(selected.id) ?? 0) : 0;
  const play = (start: string | null) => selected && void player.playList(ids, start, { kind: 'artist', id: selected.id, label: selected.name });

  return (
    <>
      {/* Add songs fills in the selected artist; the switch is the library's (no per-artist scope). */}
      <PageHeader title={t('artists.title')} sub={t('count.artists', { n: artists.length })} actions={headerActions(selected?.id)} />
      <div style={{ marginTop: 24, display: 'flex', flexWrap: 'wrap', gap: 24, alignItems: 'flex-start' }}>
        <ul aria-label={t('artists.title')} style={{ flex: '1 1 260px', maxWidth: '100%', listStyle: 'none', margin: 0, padding: 8, borderRadius: 20, background: 'var(--well)', display: 'flex', flexDirection: 'column', gap: 2, maxHeight: '70vh', overflowY: 'auto' }}>
          {artists.map((a) => {
            const on = a.id === selected?.id;
            return (
              <li key={a.id}>
                <button
                  type="button"
                  aria-current={on}
                  onClick={() => navigate(`/artists/${a.id}`, { replace: !!id })}
                  style={{ display: 'flex', alignItems: 'center', gap: 12, width: '100%', minHeight: 60, padding: '6px 12px 6px 8px', border: 0, borderRadius: 14, color: 'var(--white)', background: on ? 'var(--charcoal)' : 'transparent', font: 'inherit', textAlign: 'left', cursor: 'pointer' }}
                >
                  <span aria-hidden="true" style={{ width: 44, height: 44, borderRadius: '50%', flex: 'none', display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: 'var(--font-display)', fontStretch: '125%', fontWeight: 800, fontSize: 17, background: on ? 'var(--sand)' : 'var(--charcoal)', color: on ? 'var(--ink)' : 'var(--sand)' }}>
                    {a.name.charAt(0).toUpperCase()}
                  </span>
                  <span style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0 }}>
                    <span className="ellipsis" style={{ fontSize: 15, fontWeight: on ? 700 : 500 }}>
                      {a.name}
                    </span>
                    <span style={{ fontSize: 12, color: 'var(--muted-2)' }}>{t('count.songs', { n: counts.get(a.id) ?? 0 })}</span>
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
        {selected && (
          <section aria-labelledby="art-name" style={{ flex: '999 1 560px', minWidth: 0 }}>
            <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 20 }}>
              <span aria-hidden="true" className="vinyl" style={{ position: 'relative', width: 116, height: 116, flex: 'none', boxShadow: '0 18px 30px -14px rgba(0,0,0,.85)' }}>
                <span style={{ position: 'absolute', width: 56, height: 56, borderRadius: '50%', background: 'var(--sand)', color: 'var(--ink)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: 'var(--font-display)', fontStretch: '125%', fontWeight: 800, fontSize: 24, zIndex: 1 }}>
                  {selected.name.charAt(0).toUpperCase()}
                </span>
              </span>
              <div style={{ flex: '1 1 220px', minWidth: 0 }}>
                <div className="kicker">{t('artists.kicker')}</div>
                <h2 id="art-name" style={{ margin: '6px 0 0', fontFamily: 'var(--font-display)', fontStretch: '112%', fontWeight: 800, fontSize: 34, letterSpacing: '-0.02em', lineHeight: 1.1, overflowWrap: 'anywhere' }}>
                  {selected.name}
                </h2>
                <p style={{ margin: '6px 0 0', fontSize: 14, color: 'var(--muted)' }}>{t('artists.order', { count: t('count.songs', { n: count }) })}</p>
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                <button type="button" className="btn btn-primary" disabled={songs.length === 0} onClick={() => play(null)}>
                  <Icon name="play" size={18} />
                  {t('common.play')}
                </button>
                <button type="button" className="btn btn-secondary btn-sm" style={{ minHeight: 44 }} onClick={() => dialogs.open({ kind: 'renameArtist', id: selected.id })}>
                  <Icon name="pencil" size={16} />
                  {t('common.rename')}
                </button>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  style={{ minHeight: 44 }}
                  disabled={count > 0}
                  title={count > 0 ? t('artists.deleteHint') : undefined}
                  onClick={() => dialogs.open({ kind: 'deleteArtist', id: selected.id })}
                >
                  <Icon name="trash" size={16} />
                  {t('common.delete')}
                </button>
              </div>
            </header>
            {count > 0 && <p style={{ margin: '14px 0 0', fontSize: 13, color: 'var(--muted-2)' }}>{t('artists.deleteHint')}</p>}
            {songs.length === 0 ? (
              <EmptyState title={t('artists.noSongs')} action={<Link to="/songs" className="btn btn-secondary">{t('nav.songs')}</Link>} />
            ) : (
              <div className="table-scroll" style={{ marginTop: 18 }}>
                <div style={{ minWidth: 560, display: 'flex', flexDirection: 'column', gap: 6 }}>
                  <div aria-hidden="true" className="grid-head" style={{ gridTemplateColumns: songColumns(false, false) }}>
                    <span className="th">{t('songs.colTitle')}</span>
                    <span className="th">{t('songs.colStatus')}</span>
                    <span className="th" style={{ textAlign: 'right' }}>
                      {t('songs.colTime')}
                    </span>
                    <span />
                  </div>
                  <IncrementalList
                    items={songs}
                    getId={(x) => x.id}
                    label={t('artists.songsBy', { name: selected.name })}
                    render={(x) => <SongRow track={x} playing={x.id === nowPlaying} onPlay={() => play(x.id)} ctx={{ showArtist: false }} />}
                  />
                </div>
              </div>
            )}
          </section>
        )}
      </div>
    </>
  );
}

function headerActions(artistId: string | undefined) {
  return (
    <>
      <SyncControl spec={{ scope: 'library' }} />
      <AddSongsButton artistId={artistId} />
    </>
  );
}
