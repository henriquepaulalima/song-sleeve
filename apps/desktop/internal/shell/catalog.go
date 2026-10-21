package shell

import (
	"context"
	"fmt"
	"strings"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/library"
)

// Local songs reach the UI as tracks with these ID prefixes, so the shared screens
// list, play and edit them like synced songs; the shell routes their changes to the
// local library instead of the server.
const (
	localPrefix       = "local:"
	localArtistPrefix = "local-artist:"
)

func localID(id string) string { return localPrefix + id }

func isLocal(id string) bool { return strings.HasPrefix(id, localPrefix) }

func songID(id string) string { return strings.TrimPrefix(id, localPrefix) }

func formatLarge(n int) string {
	return fmt.Sprintf("The folder check found %d files that aren't synced songs. Only synced songs belong in the Song Sleeve folder.", n)
}

// DeviceInfo is what this device knows about a song: the "On this device" badge,
// local health and sync state for local songs, and whether a synced song's file is
// already in the Songs folder.
type DeviceInfo struct {
	Local       bool    `json:"local"`
	Health      string  `json:"health,omitempty"`
	SyncState   string  `json:"sync_state,omitempty"`
	SyncFailure *string `json:"sync_failure,omitempty"`
	SourcePath  string  `json:"source_path,omitempty"`
	Downloaded  bool    `json:"downloaded,omitempty"`
}

type catalogTrack struct {
	api.Track
	Device DeviceInfo `json:"device"`
}

type catalogDoc struct {
	Cursor      int64             `json:"cursor"`
	Tracks      []catalogTrack    `json:"tracks"`
	Artists     []api.Artist      `json:"artists"`
	Playlists   []api.Playlist    `json:"playlists"`
	Groups      []api.Group       `json:"groups"`
	Preferences *api.Preferences  `json:"preferences,omitempty"`
	Scopes      []api.Scope       `json:"scopes"`
	Folder      map[string]string `json:"folder"`
}

func songTrack(s *library.Song) catalogTrack {
	t := api.Track{
		ID: localID(s.ID), Name: s.Name, DurationMs: s.DurationMs, ByteSize: s.ByteSize, Format: s.Format,
		SHA256: s.SHA256, State: "ready", LibraryPosition: s.LibraryPosition, CreatedAt: s.AddedAt,
		MetadataSuggestion: []byte("null"),
	}
	switch {
	case s.ArtistID != nil:
		t.ArtistID = s.ArtistID
	case s.ArtistName != nil:
		id := localArtistPrefix + library.NormalizeArtist(*s.ArtistName)
		t.ArtistID = &id
	}
	return catalogTrack{Track: t, Device: DeviceInfo{Local: true, Health: s.Health, SyncState: s.SyncState, SyncFailure: s.SyncFailure, SourcePath: s.SourcePath}}
}

// catalog builds the merged lists for the UI: the replica plus this device's local
// songs and overlay entries (§8.2).
func (a *account) catalog(ctx context.Context) (*catalogDoc, error) {
	items, err := a.lib.Library(ctx)
	if err != nil {
		return nil, err
	}
	files, err := a.downloaded(ctx)
	if err != nil {
		return nil, err
	}
	doc := &catalogDoc{Tracks: make([]catalogTrack, 0, len(items))}
	localArtists := map[string]string{}
	for _, it := range items {
		if it.Track != nil {
			doc.Tracks = append(doc.Tracks, catalogTrack{Track: *it.Track, Device: DeviceInfo{Downloaded: files[it.ID]}})
			continue
		}
		t := songTrack(it.Song)
		if it.Song.ArtistID == nil && it.Song.ArtistName != nil {
			localArtists[*t.ArtistID] = *it.Song.ArtistName
		}
		doc.Tracks = append(doc.Tracks, t)
	}
	if doc.Cursor, err = a.db.Cursor(ctx); err != nil {
		return nil, err
	}
	if doc.Artists, err = a.db.Artists(ctx); err != nil {
		return nil, err
	}
	for id, name := range localArtists {
		doc.Artists = append(doc.Artists, api.Artist{ID: id, Name: name})
	}
	pls, err := a.db.Playlists(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pls {
		merged, err := a.mergedPlaylist(ctx, p)
		if err != nil {
			return nil, err
		}
		doc.Playlists = append(doc.Playlists, *merged)
	}
	if doc.Groups, err = a.db.Groups(ctx); err != nil {
		return nil, err
	}
	var prefs api.Preferences
	if ok, err := a.db.GetMeta(ctx, "preferences", &prefs); err != nil {
		return nil, err
	} else if ok {
		doc.Preferences = &prefs
	}
	if doc.Scopes, err = a.db.Scopes(ctx); err != nil {
		return nil, err
	}
	doc.Folder = map[string]string{"root": a.songs.Root, "account": a.songs.AccountDir}
	return doc, nil
}

// mergedPlaylist returns a playlist with this device's overlay entries merged in.
func (a *account) mergedPlaylist(ctx context.Context, p api.Playlist) (*api.Playlist, error) {
	items, err := a.lib.Playlist(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := p
	out.Items = make([]api.PlaylistItem, 0, len(items))
	for _, it := range items {
		id := it.ID
		if it.Kind == "local" {
			id = localID(id)
		}
		out.Items = append(out.Items, api.PlaylistItem{TrackID: id, Position: it.Position})
	}
	return &out, nil
}

func (a *account) downloaded(ctx context.Context) (map[string]bool, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT track_id FROM synced_files WHERE readiness IN ('ready', 'pending_apply', 'cleanup_pending') AND file_name IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
