package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
)

func playlistDoc(ctx context.Context, tx *sql.Tx, id string) (*api.Playlist, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT doc FROM replica_playlists WHERE id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p api.Playlist
	return &p, json.Unmarshal([]byte(raw), &p)
}

// Item is one row of a merged list: a synced track or a local song. Local songs
// carry the "On this device" badge (§8.2).
type Item struct {
	Kind     string     `json:"kind"` // "synced" | "local"
	ID       string     `json:"id"`
	Position string     `json:"position"`
	Track    *api.Track `json:"track,omitempty"`
	Song     *Song      `json:"song,omitempty"`
}

func sortItems(items []Item) {
	// Merge by key, ties broken by ID (R-ORG-6).
	sort.Slice(items, func(i, j int) bool {
		if items[i].Position != items[j].Position {
			return items[i].Position < items[j].Position
		}
		return items[i].ID < items[j].ID
	})
}

// Library returns the merged library order: the server replica plus this device's
// local songs. Songs being synced appear once, as the server track.
func (l *Library) Library(ctx context.Context) ([]Item, error) {
	tracks, err := l.db.Tracks(ctx)
	if err != nil {
		return nil, err
	}
	songs, err := l.Songs(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(tracks)+len(songs))
	for i := range tracks {
		t := tracks[i]
		items = append(items, Item{Kind: "synced", ID: t.ID, Position: t.LibraryPosition, Track: &t})
	}
	for i := range songs {
		s := songs[i]
		if s.SyncState == "syncing" && s.SyncingTrackID != nil {
			continue
		}
		items = append(items, Item{Kind: "local", ID: s.ID, Position: s.LibraryPosition, Song: &s})
	}
	sortItems(items)
	return items, nil
}

// Playlist returns a playlist's merged items: its synced songs plus this device's
// overlay entries. Other devices see only the synced songs (R-ORG-4).
func (l *Library) Playlist(ctx context.Context, playlistID string) ([]Item, error) {
	p, err := l.db.Playlist(ctx, playlistID)
	if err != nil || p == nil {
		return nil, err
	}
	var items []Item
	for _, it := range p.Items {
		t, err := l.db.Track(ctx, it.TrackID)
		if err != nil {
			return nil, err
		}
		if t != nil {
			items = append(items, Item{Kind: "synced", ID: t.ID, Position: it.Position, Track: t})
		}
	}
	rows, err := l.db.QueryContext(ctx, `SELECT local_song_id, position FROM local_playlist_items WHERE playlist_id = ?`, playlistID)
	if err != nil {
		return nil, err
	}
	type entry struct{ id, pos string }
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.pos); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	rows.Close()
	for _, e := range entries {
		s, err := l.Song(ctx, e.id)
		if err != nil {
			return nil, err
		}
		items = append(items, Item{Kind: "local", ID: s.ID, Position: e.pos, Song: s})
	}
	sortItems(items)
	return items, nil
}

// OverlayEntries returns a local song's playlist memberships, which a sync submits
// as server memberships (R-ORG-4).
func (l *Library) OverlayEntries(ctx context.Context, songID string) ([]string, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT playlist_id FROM local_playlist_items WHERE local_song_id = ? ORDER BY position`, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ArtistNames lists artist names used only by this device's local songs, for the
// Artists screen and the smart artist search (R-ORG-2).
func (l *Library) LocalOnlyArtistNames(ctx context.Context) ([]string, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT DISTINCT artist_name FROM local_songs WHERE artist_name IS NOT NULL AND artist_id IS NULL ORDER BY artist_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
