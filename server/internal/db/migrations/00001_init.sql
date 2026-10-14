-- +goose Up
-- Song Sleeve initial schema (§5.1). Every user-owned table carries user_id and
-- relationships use composite (user_id, id) foreign keys (R-MUT-6).
-- No table stores anything about local songs (R-OWN-1).

-- Accounts and devices ---------------------------------------------------------

CREATE TABLE users (
    id                      uuid PRIMARY KEY,
    email                   text NOT NULL,
    password_hash           text NOT NULL,
    terms_accepted_at       timestamptz NOT NULL,
    terms_version           text NOT NULL,
    locale                  text NOT NULL DEFAULT 'en' CHECK (locale IN ('en', 'pt-BR')),
    cloud_sync_enabled      boolean NOT NULL DEFAULT false,
    storage_quota_bytes     bigint NOT NULL,
    storage_used_bytes      bigint NOT NULL DEFAULT 0 CHECK (storage_used_bytes >= 0),
    storage_reserved_bytes  bigint NOT NULL DEFAULT 0 CHECK (storage_reserved_bytes >= 0),
    suspended_at            timestamptz,
    deletion_requested_at   timestamptz,
    events_purged_through   bigint NOT NULL DEFAULT 0,
    revision                bigint NOT NULL DEFAULT 1,
    created_at              timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (email);

CREATE TABLE devices (
    id               uuid PRIMARY KEY,
    user_id          uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    installation_id  text NOT NULL,
    platform         text NOT NULL CHECK (platform IN ('web', 'desktop', 'android', 'ios')),
    label            text NOT NULL,
    last_seen_at     timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id),
    UNIQUE (user_id, installation_id)
);

CREATE TABLE user_sessions (
    id              uuid PRIMARY KEY,
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_id       uuid NOT NULL,
    token_hash      bytea NOT NULL UNIQUE,
    transport       text NOT NULL CHECK (transport IN ('cookie', 'bearer')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    last_active_at  timestamptz NOT NULL DEFAULT now(),
    revoked_at      timestamptz,
    FOREIGN KEY (user_id, device_id) REFERENCES devices (user_id, id) ON DELETE CASCADE
);
CREATE INDEX user_sessions_user_idx ON user_sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE password_reset_tokens (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  bytea NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE reauthentication_proofs (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    session_id  uuid NOT NULL REFERENCES user_sessions (id) ON DELETE CASCADE,
    token_hash  bytea NOT NULL UNIQUE,
    purpose     text NOT NULL CHECK (purpose IN ('delete_account')),
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Fixed-window counters shared by every API replica (§4.4: never in process memory).
CREATE TABLE rate_limit_buckets (
    key           text NOT NULL,
    window_start  timestamptz NOT NULL,
    count         integer NOT NULL,
    PRIMARY KEY (key, window_start)
);

CREATE TABLE admin_actions (
    id          bigserial PRIMARY KEY,
    operator    text NOT NULL,
    action      text NOT NULL,
    user_id     uuid,
    arguments   jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Not cascaded with users: only a minimal ledger remains after deletion (§5.1).
CREATE TABLE account_deletion_requests (
    id                uuid PRIMARY KEY,
    original_user_id  uuid NOT NULL,
    phase             text NOT NULL CHECK (phase IN ('accepted', 'draining', 'wiping', 'completed')),
    checkpoints       jsonb NOT NULL DEFAULT '{}',
    requested_at      timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz
);
CREATE INDEX account_deletion_requests_user_idx ON account_deletion_requests (original_user_id);

CREATE TABLE device_cleanup_receipts (
    id                   uuid PRIMARY KEY,
    original_user_id     uuid NOT NULL,
    device_id            uuid NOT NULL,
    credential_hash      bytea NOT NULL UNIQUE,
    deletion_request_id  uuid REFERENCES account_deletion_requests (id),
    acknowledged_at      timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now()
);

-- Catalog and organization (synced songs only) ---------------------------------

CREATE TABLE artists (
    id               uuid PRIMARY KEY,
    user_id          uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name             text NOT NULL,
    normalized_name  text NOT NULL,
    revision         bigint NOT NULL DEFAULT 1,
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id),
    UNIQUE (user_id, normalized_name)
);

CREATE TABLE tracks (
    id                  uuid PRIMARY KEY,
    user_id             uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name                text NOT NULL,
    artist_id           uuid,
    duration_ms         integer NOT NULL CHECK (duration_ms > 0 AND duration_ms <= 1800000),
    byte_size           bigint NOT NULL CHECK (byte_size > 0 AND byte_size <= 134217728),
    format              text NOT NULL CHECK (format IN ('mp3', 'aac', 'flac', 'wav')),
    sha256              text NOT NULL,
    state               text NOT NULL CHECK (state IN ('pending_upload', 'ready')),
    current_content_id  uuid,
    origin_device_id    uuid,
    library_position    text NOT NULL COLLATE "C",
    -- A replacement field in conflict (R-MUT-5): the user's choice shown again as a suggestion.
    metadata_suggestion jsonb,
    revision            bigint NOT NULL DEFAULT 1,
    pending_since       timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id),
    FOREIGN KEY (user_id, artist_id) REFERENCES artists (user_id, id)
);
CREATE INDEX tracks_user_position_idx ON tracks (user_id, library_position, id);
CREATE INDEX tracks_user_artist_idx ON tracks (user_id, artist_id);

CREATE TABLE track_content (
    id                     uuid PRIMARY KEY,
    user_id                uuid NOT NULL,
    track_id               uuid NOT NULL,
    sha256                 text NOT NULL,
    byte_size              bigint NOT NULL,
    duration_ms            integer NOT NULL,
    format                 text NOT NULL,
    object_key             text NOT NULL,
    state                  text NOT NULL CHECK (state IN ('candidate', 'current', 'retired')),
    last_grant_expires_at  timestamptz,
    created_at             timestamptz NOT NULL DEFAULT now(),
    retired_at             timestamptz,
    UNIQUE (user_id, id),
    FOREIGN KEY (user_id, track_id) REFERENCES tracks (user_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX track_content_one_current_idx ON track_content (track_id) WHERE state = 'current';

ALTER TABLE tracks
    ADD CONSTRAINT tracks_current_content_fk
    FOREIGN KEY (user_id, current_content_id) REFERENCES track_content (user_id, id)
    ON DELETE SET NULL (current_content_id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE playlists (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL,
    revision    bigint NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id)
);

CREATE TABLE playlist_items (
    user_id      uuid NOT NULL,
    playlist_id  uuid NOT NULL,
    track_id     uuid NOT NULL,
    position     text NOT NULL COLLATE "C",
    added_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (playlist_id, track_id),
    FOREIGN KEY (user_id, playlist_id) REFERENCES playlists (user_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, track_id) REFERENCES tracks (user_id, id) ON DELETE CASCADE
);
CREATE INDEX playlist_items_track_idx ON playlist_items (track_id);

CREATE TABLE groups (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL,
    revision    bigint NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id)
);

CREATE TABLE group_playlists (
    user_id      uuid NOT NULL,
    group_id     uuid NOT NULL,
    playlist_id  uuid NOT NULL,
    position     text NOT NULL COLLATE "C",
    added_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, playlist_id),
    FOREIGN KEY (user_id, group_id) REFERENCES groups (user_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, playlist_id) REFERENCES playlists (user_id, id) ON DELETE CASCADE
);
CREATE INDEX group_playlists_playlist_idx ON group_playlists (playlist_id);

CREATE TABLE library_view_preferences (
    user_id    uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    songs      boolean NOT NULL DEFAULT true,
    playlists  boolean NOT NULL DEFAULT true,
    groups     boolean NOT NULL DEFAULT true,
    revision   bigint NOT NULL DEFAULT 1,
    CHECK (songs OR playlists OR groups)
);

CREATE TABLE sync_scopes (
    id             uuid PRIMARY KEY,
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scope_type     text NOT NULL CHECK (scope_type IN ('library', 'playlist', 'group')),
    -- The nil UUID stands for the whole library.
    collection_id  uuid NOT NULL,
    state          text NOT NULL CHECK (state IN ('enabled', 'paused')),
    revision       bigint NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, scope_type, collection_id)
);

-- Mutations, operations and events ----------------------------------------------

CREATE TABLE mutation_receipts (
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    request_id    text NOT NULL,
    status        text NOT NULL CHECK (status IN ('committed', 'rejected')),
    http_status   integer NOT NULL,
    result        jsonb NOT NULL,
    operation_id  uuid,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, request_id)
);
CREATE INDEX mutation_receipts_created_idx ON mutation_receipts (created_at);

CREATE TABLE track_sync_operations (
    id                    uuid PRIMARY KEY,
    user_id               uuid NOT NULL,
    track_id              uuid NOT NULL,
    kind                  text NOT NULL CHECK (kind IN ('initial_upload', 'replace', 'delete')),
    state                 text NOT NULL CHECK (state IN ('waiting', 'active', 'completed', 'failed', 'cancelled')),
    phase                 text NOT NULL DEFAULT 'uploading' CHECK (phase IN ('uploading', 'verifying')),
    request_id            text NOT NULL,
    owner_device_id       uuid,
    candidate_content_id  uuid,
    staging_key           text,
    multipart_upload_id   text,
    declared_bytes        bigint,
    declared_sha256       text,
    declared_duration_ms  integer,
    declared_format       text,
    -- Replacement metadata chosen in the suggestion step (R-MUT-5) and the values
    -- the user saw as current when choosing.
    chosen_metadata       jsonb,
    base_metadata         jsonb,
    reserved_bytes        bigint NOT NULL DEFAULT 0,
    confirmed_bytes       bigint NOT NULL DEFAULT 0,
    last_progress_at      timestamptz,
    last_hint_at          timestamptz,
    heartbeat_at          timestamptz,
    grant_expires_at      timestamptz,
    failure_reason        text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    activated_at          timestamptz,
    finished_at           timestamptz,
    UNIQUE (user_id, id),
    FOREIGN KEY (user_id, track_id) REFERENCES tracks (user_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX track_sync_operations_active_idx ON track_sync_operations (user_id, track_id) WHERE state = 'active';
CREATE INDEX track_sync_operations_waiting_idx ON track_sync_operations (user_id, track_id, created_at) WHERE state = 'waiting';
CREATE INDEX track_sync_operations_stall_idx ON track_sync_operations (last_progress_at) WHERE state = 'active';

CREATE TABLE account_events (
    cursor       bigserial PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    entity_type  text NOT NULL,
    entity_id    uuid NOT NULL,
    event_type   text NOT NULL,
    revision     bigint NOT NULL DEFAULT 0,
    payload      jsonb NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX account_events_user_cursor_idx ON account_events (user_id, cursor);
CREATE INDEX account_events_created_idx ON account_events (created_at);

-- +goose Down
DROP TABLE account_events;
DROP TABLE track_sync_operations;
DROP TABLE mutation_receipts;
DROP TABLE sync_scopes;
DROP TABLE library_view_preferences;
DROP TABLE group_playlists;
DROP TABLE groups;
DROP TABLE playlist_items;
DROP TABLE playlists;
ALTER TABLE tracks DROP CONSTRAINT tracks_current_content_fk;
DROP TABLE track_content;
DROP TABLE tracks;
DROP TABLE artists;
DROP TABLE device_cleanup_receipts;
DROP TABLE account_deletion_requests;
DROP TABLE admin_actions;
DROP TABLE rate_limit_buckets;
DROP TABLE reauthentication_proofs;
DROP TABLE password_reset_tokens;
DROP TABLE user_sessions;
DROP TABLE devices;
DROP TABLE users;
