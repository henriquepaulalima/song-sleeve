package accountdb

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
)

// TrackChange tells reconciliation and the player what happened to a track.
type TrackChange struct {
	ID      string
	Deleted bool
	Before  *api.Track
	After   *api.Track
}

// ApplyResult summarizes one applied snapshot or change page.
type ApplyResult struct {
	Tracks           []TrackChange
	DeletedPlaylists []string
	SyncFailures     []api.ChangeEvent
	AccountChanged   bool
}

func loadTrack(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (*api.Track, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT doc FROM replica_tracks WHERE id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t api.Track
	return &t, json.Unmarshal([]byte(raw), &t)
}

func putTrack(ctx context.Context, tx *sql.Tx, t *api.Track) error {
	raw, _ := json.Marshal(t)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO replica_tracks (id, revision, library_position, artist_id, state, content_id, doc) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET revision = excluded.revision, library_position = excluded.library_position,
			artist_id = excluded.artist_id, state = excluded.state, content_id = excluded.content_id, doc = excluded.doc`,
		t.ID, t.Revision, t.LibraryPosition, t.ArtistID, t.State, t.CurrentContentID, string(raw))
	return err
}

func putDoc(ctx context.Context, tx *sql.Tx, table, id string, rev int64, doc any) error {
	raw, _ := json.Marshal(doc)
	_, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (id, revision, doc) VALUES (?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET revision = excluded.revision, doc = excluded.doc WHERE excluded.revision >= `+table+`.revision`, id, rev, string(raw))
	return err
}

func putArtist(ctx context.Context, tx *sql.Tx, a api.Artist) error {
	raw, _ := json.Marshal(a)
	_, err := tx.ExecContext(ctx, `INSERT INTO replica_artists (id, revision, name, doc) VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET revision = excluded.revision, name = excluded.name, doc = excluded.doc WHERE excluded.revision >= replica_artists.revision`,
		a.ID, a.Revision, a.Name, string(raw))
	return err
}

// ApplySnapshot replaces the replica with a full snapshot (all pages).
func (d *DB) ApplySnapshot(ctx context.Context, pages []api.Snapshot) (*ApplyResult, error) {
	res := &ApplyResult{}
	if len(pages) == 0 {
		return res, nil
	}
	first := pages[0]
	err := d.InTx(ctx, func(tx *sql.Tx) error {
		before := map[string]*api.Track{}
		rows, err := tx.QueryContext(ctx, `SELECT doc FROM replica_tracks`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			var t api.Track
			_ = json.Unmarshal([]byte(raw), &t)
			before[t.ID] = &t
		}
		rows.Close()
		for _, table := range []string{"replica_tracks", "replica_artists", "replica_playlists", "replica_groups"} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table); err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		for _, p := range pages {
			for i := range p.Tracks {
				t := p.Tracks[i]
				if err := putTrack(ctx, tx, &t); err != nil {
					return err
				}
				seen[t.ID] = true
				res.Tracks = append(res.Tracks, TrackChange{ID: t.ID, Before: before[t.ID], After: &t})
			}
		}
		for id, t := range before {
			if !seen[id] {
				res.Tracks = append(res.Tracks, TrackChange{ID: id, Deleted: true, Before: t})
			}
		}
		for _, a := range first.Artists {
			if err := putArtist(ctx, tx, a); err != nil {
				return err
			}
		}
		playlists := map[string]bool{}
		for _, p := range first.Playlists {
			playlists[p.ID] = true
			if err := putDoc(ctx, tx, "replica_playlists", p.ID, p.Revision, p); err != nil {
				return err
			}
		}
		for _, g := range first.Groups {
			if err := putDoc(ctx, tx, "replica_groups", g.ID, g.Revision, g); err != nil {
				return err
			}
		}
		if first.Preferences != nil {
			if err := SetMeta(ctx, tx, "preferences", first.Preferences); err != nil {
				return err
			}
		}
		if err := SetMeta(ctx, tx, "scopes", first.Scopes); err != nil {
			return err
		}
		cursor := int64(0)
		if first.Cursor != nil {
			cursor = *first.Cursor
		}
		if err := SetMeta(ctx, tx, "cursor", cursor); err != nil {
			return err
		}
		// Overlay entries for playlists that no longer exist are dropped (§7.9 step 2).
		dropped, err := dropOrphanOverlay(ctx, tx, playlists)
		res.DeletedPlaylists = dropped
		return err
	})
	return res, err
}

func dropOrphanOverlay(ctx context.Context, tx *sql.Tx, keep map[string]bool) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT playlist_id FROM local_playlist_items`)
	if err != nil {
		return nil, err
	}
	var gone []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := tx.ExecContext(ctx, `DELETE FROM local_playlist_items WHERE playlist_id = ?`, id); err != nil {
			return nil, err
		}
	}
	return gone, nil
}

// ApplyChanges applies one page of the change feed. Older revisions are ignored and
// re-applying a page is harmless (R-EVT-3). The cursor advances in the same
// transaction as the data (R-EVT-3: only after the events are saved).
func (d *DB) ApplyChanges(ctx context.Context, ch *api.Changes) (*ApplyResult, error) {
	res := &ApplyResult{AccountChanged: ch.AccountChanged}
	err := d.InTx(ctx, func(tx *sql.Tx) error {
		for i := range ch.Tracks {
			t := ch.Tracks[i]
			before, err := loadTrack(ctx, tx, t.ID)
			if err != nil {
				return err
			}
			if before != nil && t.Revision < before.Revision {
				continue
			}
			if err := putTrack(ctx, tx, &t); err != nil {
				return err
			}
			res.Tracks = append(res.Tracks, TrackChange{ID: t.ID, Before: before, After: &t})
		}
		for _, id := range ch.Deleted["track"] {
			before, err := loadTrack(ctx, tx, id)
			if err != nil {
				return err
			}
			if before == nil {
				continue
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM replica_tracks WHERE id = ?`, id); err != nil {
				return err
			}
			res.Tracks = append(res.Tracks, TrackChange{ID: id, Deleted: true, Before: before})
		}
		for _, a := range ch.Artists {
			if err := putArtist(ctx, tx, a); err != nil {
				return err
			}
			// A renamed server artist renames the local songs linked to it (F-10).
			if _, err := tx.ExecContext(ctx, `UPDATE local_songs SET artist_name = ? WHERE artist_id = ?`, a.Name, a.ID); err != nil {
				return err
			}
		}
		for _, id := range ch.Deleted["artist"] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM replica_artists WHERE id = ?`, id); err != nil {
				return err
			}
			// Local songs keep the name and lose the link (F-10).
			if _, err := tx.ExecContext(ctx, `UPDATE local_songs SET artist_id = NULL WHERE artist_id = ?`, id); err != nil {
				return err
			}
		}
		for _, p := range ch.Playlists {
			if err := putDoc(ctx, tx, "replica_playlists", p.ID, p.Revision, p); err != nil {
				return err
			}
		}
		for _, id := range ch.Deleted["playlist"] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM replica_playlists WHERE id = ?`, id); err != nil {
				return err
			}
			// Another device deleted the playlist: drop this device's overlay entries (§7.6).
			if _, err := tx.ExecContext(ctx, `DELETE FROM local_playlist_items WHERE playlist_id = ?`, id); err != nil {
				return err
			}
			res.DeletedPlaylists = append(res.DeletedPlaylists, id)
		}
		for _, g := range ch.Groups {
			if err := putDoc(ctx, tx, "replica_groups", g.ID, g.Revision, g); err != nil {
				return err
			}
		}
		for _, id := range ch.Deleted["group"] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM replica_groups WHERE id = ?`, id); err != nil {
				return err
			}
		}
		if ch.Preferences != nil {
			if err := SetMeta(ctx, tx, "preferences", ch.Preferences); err != nil {
				return err
			}
		}
		if ch.Scopes != nil {
			if err := SetMeta(ctx, tx, "scopes", ch.Scopes); err != nil {
				return err
			}
		}
		for _, e := range ch.Events {
			if e.EventType == "track_sync_failed" {
				res.SyncFailures = append(res.SyncFailures, e)
			}
		}
		var cur int64
		if _, err := getMetaTx(ctx, tx, "cursor", &cur); err != nil {
			return err
		}
		return SetMeta(ctx, tx, "cursor", max(cur, ch.Cursor))
	})
	return res, err
}

func getMetaTx(ctx context.Context, tx *sql.Tx, key string, dst any) (bool, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value FROM replica_meta WHERE key = ?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), dst)
}

// Cursor returns the stored catalog cursor.
func (d *DB) Cursor(ctx context.Context) (int64, error) {
	var c int64
	_, err := d.GetMeta(ctx, "cursor", &c)
	return c, err
}

// Track returns one replica track, or nil.
func (d *DB) Track(ctx context.Context, id string) (*api.Track, error) {
	return loadTrack(ctx, d.DB, id)
}

// Tracks returns every replica track in library order.
func (d *DB) Tracks(ctx context.Context) ([]api.Track, error) {
	rows, err := d.QueryContext(ctx, `SELECT doc FROM replica_tracks ORDER BY library_position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.Track
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var t api.Track
		if err := json.Unmarshal([]byte(raw), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) Artists(ctx context.Context) ([]api.Artist, error) {
	rows, err := d.QueryContext(ctx, `SELECT doc FROM replica_artists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.Artist
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var a api.Artist
		_ = json.Unmarshal([]byte(raw), &a)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) Playlists(ctx context.Context) ([]api.Playlist, error) {
	rows, err := d.QueryContext(ctx, `SELECT doc FROM replica_playlists`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.Playlist
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p api.Playlist
		_ = json.Unmarshal([]byte(raw), &p)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) Playlist(ctx context.Context, id string) (*api.Playlist, error) {
	var raw string
	err := d.QueryRowContext(ctx, `SELECT doc FROM replica_playlists WHERE id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p api.Playlist
	return &p, json.Unmarshal([]byte(raw), &p)
}

func (d *DB) Groups(ctx context.Context) ([]api.Group, error) {
	rows, err := d.QueryContext(ctx, `SELECT doc FROM replica_groups`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.Group
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var g api.Group
		_ = json.Unmarshal([]byte(raw), &g)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (d *DB) Scopes(ctx context.Context) ([]api.Scope, error) {
	var s []api.Scope
	_, err := d.GetMeta(ctx, "scopes", &s)
	return s, err
}
