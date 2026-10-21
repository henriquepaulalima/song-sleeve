/** API documents (§6). Kept in step with the Go handlers' JSON. */

export type Platform = 'web' | 'desktop' | 'android';
export type Locale = 'en' | 'pt-BR';

export interface Me {
  id: string;
  email: string;
  locale: Locale;
  cloud_sync_enabled: boolean;
  device_id: string;
  session_id: string;
  terms_version: string;
  current_terms_version: string;
  terms_outdated: boolean;
  revision: number;
  created_at: string;
  csrf_token?: string;
}

export interface AuthResponse {
  account: Me;
  token?: string;
}

export interface OperationSummary {
  id: string;
  kind: 'initial_upload' | 'replace' | 'delete';
  state: 'waiting' | 'active';
  phase: 'uploading' | 'verifying';
  confirmed_bytes: number;
  declared_bytes: number;
  owner_device_id: string | null;
}

export interface Track {
  id: string;
  name: string;
  artist_id: string | null;
  duration_ms: number;
  byte_size: number;
  format: 'mp3' | 'aac' | 'flac' | 'wav';
  sha256: string;
  state: 'pending_upload' | 'ready';
  current_content_id: string | null;
  origin_device_id: string | null;
  library_position: string;
  metadata_suggestion: { name?: string; artist?: ArtistRef } | null;
  revision: number;
  created_at: string;
  operation: OperationSummary | null;
  /** Installed apps only: what this device knows about the song (§8.2). */
  device?: DeviceInfo;
}

/**
 * Local songs reach the shared screens as tracks whose ID starts with `local:`
 * and whose `device.local` is set; synced songs carry `downloaded` when their file
 * is already in this device's Songs folder.
 */
export interface DeviceInfo {
  local: boolean;
  health?: 'ready' | 'needs_relink' | 'source_changed' | 'access_denied';
  sync_state?: 'local_only' | 'queued' | 'syncing' | 'sync_failed' | 'needs_attention';
  sync_failure?: string | null;
  source_path?: string;
  downloaded?: boolean;
}

export interface ArtistRef {
  id?: string;
  name?: string;
}

export interface Artist {
  id: string;
  name: string;
  revision: number;
  created_at: string;
}

export interface Playlist {
  id: string;
  name: string;
  revision: number;
  created_at: string;
  items: { track_id: string; position: string }[];
}

export interface Group {
  id: string;
  name: string;
  revision: number;
  created_at: string;
  playlists: { playlist_id: string; position: string }[];
}

export interface Preferences {
  songs: boolean;
  playlists: boolean;
  groups: boolean;
  revision: number;
}

export interface Scope {
  id: string;
  type: 'library' | 'playlist' | 'group';
  collection_id: string | null;
  state: 'enabled' | 'paused';
  revision: number;
}

export interface Snapshot {
  cursor?: number;
  tracks: Track[];
  next_page_token?: string;
  artists?: Artist[];
  playlists?: Playlist[];
  groups?: Group[];
  preferences?: Preferences;
  scopes?: Scope[];
}

export interface ChangeEvent {
  cursor: number;
  entity_type: string;
  entity_id: string;
  event_type: string;
  revision: number;
  payload?: Record<string, unknown>;
}

export interface Changes {
  cursor: number;
  has_more: boolean;
  events: ChangeEvent[];
  tracks?: Track[];
  artists?: Artist[];
  playlists?: Playlist[];
  groups?: Group[];
  preferences?: Preferences;
  scopes?: Scope[];
  account_changed: boolean;
  deleted: { track?: string[]; artist?: string[]; playlist?: string[]; group?: string[] };
}

export interface OperationDoc {
  id: string;
  track_id: string;
  kind: 'initial_upload' | 'replace' | 'delete';
  state: 'waiting' | 'active' | 'completed' | 'failed' | 'cancelled';
  phase: 'uploading' | 'verifying';
  confirmed_bytes: number;
  declared_bytes: number;
  part_size: number;
  part_count: number;
  failure_reason: string | null;
  heartbeat_interval_seconds: number;
}

export interface SessionDoc {
  id: string;
  created_at: string;
  last_active_at: string;
  current: boolean;
  device: { id: string; platform: Platform | 'ios'; label: string };
}

export interface Summary {
  synced_songs: number;
  storage_used_bytes: number;
  storage_reserved_bytes: number;
  storage_quota_bytes: number;
  cloud_sync_enabled: boolean;
}

export interface MediaPolicy {
  max_duration_seconds: number;
  max_file_bytes: number;
  formats: string[];
}

/** Closed set of error codes (§4.4), plus the client-only connection_required. */
export type ErrorCode =
  | 'invalid_request'
  | 'idempotency_key_required'
  | 'unauthenticated'
  | 'csrf_failed'
  | 'forbidden'
  | 'not_found'
  | 'revision_conflict'
  | 'rate_limited'
  | 'email_unavailable'
  | 'invalid_credentials'
  | 'password_invalid'
  | 'password_too_common'
  | 'terms_required'
  | 'terms_outdated'
  | 'web_app_unavailable'
  | 'cloud_sync_unavailable'
  | 'client_update_required'
  | 'account_suspended'
  | 'account_deleted'
  | 'reauthentication_required'
  | 'reset_token_invalid'
  | 'invalid_field'
  | 'limit_reached'
  | 'song_too_long'
  | 'song_too_large'
  | 'invalid_audio_metadata'
  | 'unsupported_format'
  | 'storage_quota_exceeded'
  | 'operation_not_active'
  | 'cursor_expired'
  | 'artist_in_use'
  | 'internal'
  | 'connection_required';
