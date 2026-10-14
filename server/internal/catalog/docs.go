// Package catalog serves the account-level catalog of synced songs and the
// organization around it: artists, playlists, groups, the Library preference and
// sync scopes (R-ORG, R-OWN-4). It knows nothing about local songs (R-OWN-1).
package catalog

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
)

type OperationSummary struct {
	ID             uuid.UUID  `json:"id"`
	Kind           string     `json:"kind"`
	State          string     `json:"state"`
	Phase          string     `json:"phase"`
	ConfirmedBytes int64      `json:"confirmed_bytes"`
	DeclaredBytes  int64      `json:"declared_bytes"`
	OwnerDeviceID  *uuid.UUID `json:"owner_device_id"`
}

type Track struct {
	ID                 uuid.UUID         `json:"id"`
	Name               string            `json:"name"`
	ArtistID           *uuid.UUID        `json:"artist_id"`
	DurationMs         int               `json:"duration_ms"`
	ByteSize           int64             `json:"byte_size"`
	Format             string            `json:"format"`
	SHA256             string            `json:"sha256"`
	State              string            `json:"state"`
	CurrentContentID   *uuid.UUID        `json:"current_content_id"`
	OriginDeviceID     *uuid.UUID        `json:"origin_device_id"`
	LibraryPosition    string            `json:"library_position"`
	MetadataSuggestion json.RawMessage   `json:"metadata_suggestion"`
	Revision           int64             `json:"revision"`
	CreatedAt          time.Time         `json:"created_at"`
	Operation          *OperationSummary `json:"operation"`
}

type Artist struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
}

type PlaylistItem struct {
	TrackID  uuid.UUID `json:"track_id"`
	Position string    `json:"position"`
}

type Playlist struct {
	ID        uuid.UUID      `json:"id"`
	Name      string         `json:"name"`
	Revision  int64          `json:"revision"`
	CreatedAt time.Time      `json:"created_at"`
	Items     []PlaylistItem `json:"items"`
}

type GroupMember struct {
	PlaylistID uuid.UUID `json:"playlist_id"`
	Position   string    `json:"position"`
}

type Group struct {
	ID        uuid.UUID     `json:"id"`
	Name      string        `json:"name"`
	Revision  int64         `json:"revision"`
	CreatedAt time.Time     `json:"created_at"`
	Playlists []GroupMember `json:"playlists"`
}

type Preferences struct {
	Songs     bool  `json:"songs"`
	Playlists bool  `json:"playlists"`
	Groups    bool  `json:"groups"`
	Revision  int64 `json:"revision"`
}

type Scope struct {
	ID           uuid.UUID  `json:"id"`
	Type         string     `json:"type"`
	CollectionID *uuid.UUID `json:"collection_id"`
	State        string     `json:"state"`
	Revision     int64      `json:"revision"`
}

const trackColumns = `
	t.id, t.name, t.artist_id, t.duration_ms, t.byte_size, t.format, t.sha256, t.state,
	t.current_content_id, t.origin_device_id, t.library_position, t.metadata_suggestion, t.revision, t.created_at,
	o.id, o.kind, o.state, o.phase, o.confirmed_bytes, o.declared_bytes, o.owner_device_id`

// The track's running or queued operation, if any (Syncing… x%, Updating audio…,
// Waiting for another sync operation).
const trackOpJoin = `
	LEFT JOIN LATERAL (
		SELECT id, kind, state, phase, confirmed_bytes, COALESCE(declared_bytes, 0) AS declared_bytes, owner_device_id
		FROM track_sync_operations op
		WHERE op.user_id = t.user_id AND op.track_id = t.id AND op.state IN ('active', 'waiting')
		ORDER BY (op.state = 'active') DESC, op.created_at
		LIMIT 1
	) o ON true`

type scanner interface{ Scan(dest ...any) error }

func scanTrack(row scanner) (*Track, error) {
	var t Track
	var opID *uuid.UUID
	var kind, state, phase *string
	var confirmed, declared *int64
	var owner *uuid.UUID
	var suggestion []byte
	if err := row.Scan(&t.ID, &t.Name, &t.ArtistID, &t.DurationMs, &t.ByteSize, &t.Format, &t.SHA256, &t.State,
		&t.CurrentContentID, &t.OriginDeviceID, &t.LibraryPosition, &suggestion, &t.Revision, &t.CreatedAt,
		&opID, &kind, &state, &phase, &confirmed, &declared, &owner); err != nil {
		return nil, err
	}
	if len(suggestion) > 0 {
		t.MetadataSuggestion = suggestion
	}
	if opID != nil {
		t.Operation = &OperationSummary{ID: *opID, Kind: *kind, State: *state, Phase: *phase, ConfirmedBytes: *confirmed, DeclaredBytes: *declared, OwnerDeviceID: owner}
	}
	return &t, nil
}

// LoadTrack returns one track or db no-rows.
func LoadTrack(ctx context.Context, q db.Querier, userID, id uuid.UUID) (*Track, error) {
	return scanTrack(q.QueryRow(ctx, `SELECT `+trackColumns+` FROM tracks t `+trackOpJoin+` WHERE t.user_id = $1 AND t.id = $2`, userID, id))
}

func LoadTracks(ctx context.Context, q db.Querier, userID uuid.UUID, ids []uuid.UUID) ([]*Track, error) {
	rows, err := q.Query(ctx, `SELECT `+trackColumns+` FROM tracks t `+trackOpJoin+` WHERE t.user_id = $1 AND t.id = ANY($2) ORDER BY t.library_position, t.id`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Track{}
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TrackPage lists tracks in library order after a (position, id) cursor.
func TrackPage(ctx context.Context, q db.Querier, userID uuid.UUID, afterPos string, afterID uuid.UUID, search string, limit int) ([]*Track, error) {
	rows, err := q.Query(ctx, `
		SELECT `+trackColumns+` FROM tracks t `+trackOpJoin+`
		LEFT JOIN artists a ON a.user_id = t.user_id AND a.id = t.artist_id
		WHERE t.user_id = $1 AND (t.library_position, t.id) > ($2 COLLATE "C", $3)
		  AND ($4 = '' OR t.name ILIKE '%' || $4 || '%' OR a.name ILIKE '%' || $4 || '%')
		ORDER BY t.library_position, t.id
		LIMIT $5`, userID, afterPos, afterID, search, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Track{}
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func LoadArtists(ctx context.Context, q db.Querier, userID uuid.UUID, ids []uuid.UUID) ([]*Artist, error) {
	rows, err := q.Query(ctx, `
		SELECT id, name, revision, created_at FROM artists
		WHERE user_id = $1 AND ($2::uuid[] IS NULL OR id = ANY($2)) ORDER BY name`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Artist{}
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.Revision, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func LoadPlaylists(ctx context.Context, q db.Querier, userID uuid.UUID, ids []uuid.UUID) ([]*Playlist, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id, p.name, p.revision, p.created_at,
		       COALESCE((SELECT json_agg(json_build_object('track_id', i.track_id, 'position', i.position) ORDER BY i.position COLLATE "C", i.track_id)
		                 FROM playlist_items i WHERE i.playlist_id = p.id), '[]')
		FROM playlists p WHERE p.user_id = $1 AND ($2::uuid[] IS NULL OR p.id = ANY($2))
		ORDER BY p.created_at DESC`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Playlist{}
	for rows.Next() {
		var p Playlist
		var items []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Revision, &p.CreatedAt, &items); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(items, &p.Items); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func LoadGroups(ctx context.Context, q db.Querier, userID uuid.UUID, ids []uuid.UUID) ([]*Group, error) {
	rows, err := q.Query(ctx, `
		SELECT g.id, g.name, g.revision, g.created_at,
		       COALESCE((SELECT json_agg(json_build_object('playlist_id', m.playlist_id, 'position', m.position) ORDER BY m.position COLLATE "C", m.playlist_id)
		                 FROM group_playlists m WHERE m.group_id = g.id), '[]')
		FROM groups g WHERE g.user_id = $1 AND ($2::uuid[] IS NULL OR g.id = ANY($2))
		ORDER BY g.created_at DESC`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Group{}
	for rows.Next() {
		var g Group
		var members []byte
		if err := rows.Scan(&g.ID, &g.Name, &g.Revision, &g.CreatedAt, &members); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(members, &g.Playlists); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

func LoadPreferences(ctx context.Context, q db.Querier, userID uuid.UUID) (*Preferences, error) {
	var p Preferences
	err := q.QueryRow(ctx, `SELECT songs, playlists, groups, revision FROM library_view_preferences WHERE user_id = $1`, userID).
		Scan(&p.Songs, &p.Playlists, &p.Groups, &p.Revision)
	if db.IsNoRows(err) {
		return &Preferences{Songs: true, Playlists: true, Groups: true}, nil
	}
	return &p, err
}

func LoadScopes(ctx context.Context, q db.Querier, userID uuid.UUID) ([]*Scope, error) {
	rows, err := q.Query(ctx, `SELECT id, scope_type, collection_id, state, revision FROM sync_scopes WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Scope{}
	for rows.Next() {
		var s Scope
		var coll uuid.UUID
		if err := rows.Scan(&s.ID, &s.Type, &coll, &s.State, &s.Revision); err != nil {
			return nil, err
		}
		if coll != uuid.Nil {
			s.CollectionID = &coll
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}
