// Package api is the installed apps' client for the Song Sleeve server: bearer
// transport, the mutation contract and the document types (§6).
package api

import (
	"encoding/json"
	"time"
)

type OperationSummary struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	State          string  `json:"state"`
	Phase          string  `json:"phase"`
	ConfirmedBytes int64   `json:"confirmed_bytes"`
	DeclaredBytes  int64   `json:"declared_bytes"`
	OwnerDeviceID  *string `json:"owner_device_id"`
}

type Track struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	ArtistID           *string           `json:"artist_id"`
	DurationMs         int64             `json:"duration_ms"`
	ByteSize           int64             `json:"byte_size"`
	Format             string            `json:"format"`
	SHA256             string            `json:"sha256"`
	State              string            `json:"state"`
	CurrentContentID   *string           `json:"current_content_id"`
	OriginDeviceID     *string           `json:"origin_device_id"`
	LibraryPosition    string            `json:"library_position"`
	MetadataSuggestion json.RawMessage   `json:"metadata_suggestion"`
	Revision           int64             `json:"revision"`
	CreatedAt          time.Time         `json:"created_at"`
	Operation          *OperationSummary `json:"operation"`
}

type Artist struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
}

type PlaylistItem struct {
	TrackID  string `json:"track_id"`
	Position string `json:"position"`
}

type Playlist struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Revision  int64          `json:"revision"`
	CreatedAt time.Time      `json:"created_at"`
	Items     []PlaylistItem `json:"items"`
}

type GroupMember struct {
	PlaylistID string `json:"playlist_id"`
	Position   string `json:"position"`
}

type Group struct {
	ID        string        `json:"id"`
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
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	CollectionID *string `json:"collection_id"`
	State        string  `json:"state"`
	Revision     int64   `json:"revision"`
}

type Snapshot struct {
	Cursor        *int64       `json:"cursor"`
	Tracks        []Track      `json:"tracks"`
	NextPageToken string       `json:"next_page_token"`
	Artists       []Artist     `json:"artists"`
	Playlists     []Playlist   `json:"playlists"`
	Groups        []Group      `json:"groups"`
	Preferences   *Preferences `json:"preferences"`
	Scopes        []Scope      `json:"scopes"`
}

type ChangeEvent struct {
	Cursor     int64          `json:"cursor"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	EventType  string         `json:"event_type"`
	Revision   int64          `json:"revision"`
	Payload    map[string]any `json:"payload"`
}

type Changes struct {
	Cursor         int64               `json:"cursor"`
	HasMore        bool                `json:"has_more"`
	Events         []ChangeEvent       `json:"events"`
	Tracks         []Track             `json:"tracks"`
	Artists        []Artist            `json:"artists"`
	Playlists      []Playlist          `json:"playlists"`
	Groups         []Group             `json:"groups"`
	Preferences    *Preferences        `json:"preferences"`
	Scopes         []Scope             `json:"scopes"`
	AccountChanged bool                `json:"account_changed"`
	Deleted        map[string][]string `json:"deleted"`
}

type Me struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	Locale           string `json:"locale"`
	CloudSyncEnabled bool   `json:"cloud_sync_enabled"`
	DeviceID         string `json:"device_id"`
	SessionID        string `json:"session_id"`
	TermsVersion     string `json:"terms_version"`
	CurrentTerms     string `json:"current_terms_version"`
	TermsOutdated    bool   `json:"terms_outdated"`
	Revision         int64  `json:"revision"`
}

type AuthResponse struct {
	Account Me     `json:"account"`
	Token   string `json:"token"`
}

type OperationDoc struct {
	ID             string  `json:"id"`
	TrackID        string  `json:"track_id"`
	Kind           string  `json:"kind"`
	State          string  `json:"state"`
	Phase          string  `json:"phase"`
	ConfirmedBytes int64   `json:"confirmed_bytes"`
	DeclaredBytes  int64   `json:"declared_bytes"`
	PartSize       int64   `json:"part_size"`
	PartCount      int     `json:"part_count"`
	FailureReason  *string `json:"failure_reason"`
	HeartbeatSecs  int     `json:"heartbeat_interval_seconds"`
}

type DownloadGrant struct {
	URL      string `json:"url"`
	ByteSize int64  `json:"byte_size"`
	SHA256   string `json:"sha256"`
}
