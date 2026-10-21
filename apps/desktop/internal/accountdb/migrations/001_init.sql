-- Device-local data for one account on one installation (§5.2, R-OWN-3).
-- Server documents are kept as accepted JSON with the columns needed to query them.

CREATE TABLE replica_tracks (
    id               TEXT PRIMARY KEY,
    revision         INTEGER NOT NULL,
    library_position TEXT NOT NULL,
    artist_id        TEXT,
    state            TEXT NOT NULL,
    content_id       TEXT,
    doc              TEXT NOT NULL
);
CREATE INDEX replica_tracks_position ON replica_tracks (library_position, id);
CREATE INDEX replica_tracks_artist ON replica_tracks (artist_id);

CREATE TABLE replica_artists (
    id       TEXT PRIMARY KEY,
    revision INTEGER NOT NULL,
    name     TEXT NOT NULL,
    doc      TEXT NOT NULL
);

CREATE TABLE replica_playlists (
    id       TEXT PRIMARY KEY,
    revision INTEGER NOT NULL,
    doc      TEXT NOT NULL
);

CREATE TABLE replica_groups (
    id       TEXT PRIMARY KEY,
    revision INTEGER NOT NULL,
    doc      TEXT NOT NULL
);

-- cursor, preferences, scopes, account.
CREATE TABLE replica_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Songs that exist only on this device (R-OWN-2, R-LOCAL).
CREATE TABLE local_songs (
    id                TEXT PRIMARY KEY,
    added_at          TEXT NOT NULL,
    name              TEXT NOT NULL,
    artist_name       TEXT,
    artist_id         TEXT,  -- matched server artist, a cache only (F-10)
    duration_ms       INTEGER NOT NULL,
    byte_size         INTEGER NOT NULL,
    format            TEXT NOT NULL,
    sha256            TEXT NOT NULL,
    source_path       TEXT NOT NULL,  -- never inside the Song Sleeve folder (R-LOCAL-4)
    observed_size     INTEGER NOT NULL,
    observed_mtime    INTEGER NOT NULL,
    health            TEXT NOT NULL DEFAULT 'ready' CHECK (health IN ('ready', 'needs_relink', 'source_changed', 'access_denied')),
    library_position  TEXT NOT NULL,
    sync_state        TEXT NOT NULL DEFAULT 'local_only' CHECK (sync_state IN ('local_only', 'queued', 'syncing', 'sync_failed', 'needs_attention')),
    sync_failure      TEXT,
    syncing_track_id  TEXT
);
CREATE INDEX local_songs_position ON local_songs (library_position, id);

-- The device overlay: local songs placed in account playlists (R-ORG-4).
CREATE TABLE local_playlist_items (
    playlist_id    TEXT NOT NULL,
    local_song_id  TEXT NOT NULL REFERENCES local_songs (id) ON DELETE CASCADE,
    position       TEXT NOT NULL,
    PRIMARY KEY (playlist_id, local_song_id)
);

-- One row per synced track mirrored in the Songs folder (§7.13).
CREATE TABLE synced_files (
    track_id           TEXT PRIMARY KEY,
    content_id         TEXT,
    file_name          TEXT,
    readiness          TEXT NOT NULL CHECK (readiness IN ('ready', 'needs_download', 'downloading', 'pending_apply', 'cleanup_pending')),
    staged_content_id  TEXT,
    staged_path        TEXT,
    detail             TEXT
);

-- Accepted work still to apply, and the sync queue (R-CLOUD-9).
CREATE TABLE pending_work (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,
    subject_id  TEXT NOT NULL,
    state       TEXT NOT NULL,
    payload     TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);
CREATE INDEX pending_work_kind ON pending_work (kind, state, created_at);

-- Request IDs whose result is unknown (R-MUT-4). Not an edit queue.
CREATE TABLE outstanding_requests (
    request_id  TEXT PRIMARY KEY,
    method      TEXT NOT NULL,
    path        TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE TABLE player_state (
    id     INTEGER PRIMARY KEY CHECK (id = 1),
    state  TEXT NOT NULL
);
