package catalog

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/fields"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
	"github.com/henriquepaulalima/song-sleeve/server/internal/mutations"
	"github.com/henriquepaulalima/song-sleeve/server/pkg/position"
)

// Playlists hold songs and groups hold playlists; there is no nesting, no parents
// and no rules (R-ORG-1). Unknown fields such as "rules" or "parent_id" are rejected.

type createCollectionRequest struct {
	Name string `json:"name"`
}

func rejectExtraFields(raw map[string]any, allowed ...string) error {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	for k := range raw {
		if !ok[k] {
			return apperr.Field(k, "unsupported")
		}
	}
	return nil
}

func bindStrict(c *gin.Context, allowed ...string) (map[string]any, error) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		return nil, apperr.New(apperr.InvalidRequest, "Invalid request body")
	}
	return raw, rejectExtraFields(raw, allowed...)
}

func str(raw map[string]any, key string) string {
	s, _ := raw[key].(string)
	return s
}

func revision(raw map[string]any) (int64, error) {
	f, ok := raw["expected_revision"].(float64)
	if !ok {
		return 0, apperr.Field("expected_revision", "required")
	}
	return int64(f), nil
}

func uuids(raw map[string]any, key string) ([]uuid.UUID, error) {
	arr, ok := raw[key].([]any)
	if !ok || len(arr) == 0 {
		return nil, apperr.Field(key, "required")
	}
	if len(arr) > MaxSongsPerPlaylist {
		return nil, apperr.Field(key, "too_many")
	}
	out := make([]uuid.UUID, 0, len(arr))
	for _, v := range arr {
		s, _ := v.(string)
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, apperr.Field(key, "invalid")
		}
		out = append(out, id)
	}
	return out, nil
}

func (h *Handlers) playlistResult(ctx context.Context, tx pgx.Tx, userID, id uuid.UUID, eventType string, rev int64) (mutations.Result, error) {
	if _, err := events.Append(ctx, tx, events.Event{UserID: userID, EntityType: events.EntityPlaylist, EntityID: id, EventType: eventType, Revision: rev}); err != nil {
		return mutations.Result{}, err
	}
	docs, err := LoadPlaylists(ctx, tx, userID, []uuid.UUID{id})
	if err != nil {
		return mutations.Result{}, err
	}
	return mutations.OK(gin.H{"playlist": docs[0]}), nil
}

func (h *Handlers) groupResult(ctx context.Context, tx pgx.Tx, userID, id uuid.UUID, eventType string, rev int64) (mutations.Result, error) {
	if _, err := events.Append(ctx, tx, events.Event{UserID: userID, EntityType: events.EntityGroup, EntityID: id, EventType: eventType, Revision: rev}); err != nil {
		return mutations.Result{}, err
	}
	docs, err := LoadGroups(ctx, tx, userID, []uuid.UUID{id})
	if err != nil {
		return mutations.Result{}, err
	}
	return mutations.OK(gin.H{"group": docs[0]}), nil
}

func (h *Handlers) ListPlaylists(c *gin.Context) {
	s := httpx.CurrentSession(c)
	docs, err := LoadPlaylists(c.Request.Context(), h.d.Pool, s.UserID, nil)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"playlists": docs})
}

func (h *Handlers) CreatePlaylist(c *gin.Context) {
	raw, err := bindStrict(c, "name")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		name, err := fields.Name("name", str(raw, "name"), fields.MaxPlaylistName)
		if err != nil {
			return mutations.Result{}, err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM playlists WHERE user_id = $1`, s.UserID).Scan(&n); err != nil {
			return mutations.Result{}, err
		}
		if n >= MaxPlaylists {
			return mutations.Result{}, apperr.WithDetails(apperr.LimitReached, "Playlist limit reached", map[string]any{"limit": MaxPlaylists})
		}
		id, _ := uuid.NewV7()
		if _, err := tx.Exec(ctx, `INSERT INTO playlists (id, user_id, name) VALUES ($1, $2, $3)`, id, s.UserID, name); err != nil {
			return mutations.Result{}, err
		}
		res, err := h.playlistResult(ctx, tx, s.UserID, id, "playlist_upserted", 1)
		res.Status = http.StatusCreated
		return res, err
	})
}

func (h *Handlers) RenamePlaylist(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	raw, err := bindStrict(c, "name", "expected_revision")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		name, err := fields.Name("name", str(raw, "name"), fields.MaxPlaylistName)
		if err != nil {
			return mutations.Result{}, err
		}
		exp, err := revision(raw)
		if err != nil {
			return mutations.Result{}, err
		}
		var rev int64
		err = tx.QueryRow(ctx, `UPDATE playlists SET name = $4, revision = revision + 1 WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`,
			s.UserID, id, exp, name).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "playlists", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		return h.playlistResult(ctx, tx, s.UserID, id, "playlist_upserted", rev)
	})
}

// DeletePlaylist keeps its songs and takes it out of its groups (R-ORG-5).
func (h *Handlers) DeletePlaylist(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		rows, err := tx.Query(ctx, `SELECT group_id FROM group_playlists WHERE user_id = $1 AND playlist_id = $2`, s.UserID, id)
		if err != nil {
			return mutations.Result{}, err
		}
		groupIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return mutations.Result{}, err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM playlists WHERE user_id = $1 AND id = $2`, s.UserID, id)
		if err != nil {
			return mutations.Result{}, err
		}
		if tag.RowsAffected() == 0 {
			return mutations.Result{}, apperr.New(apperr.NotFound, "Playlist not found")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sync_scopes WHERE user_id = $1 AND scope_type = 'playlist' AND collection_id = $2`, s.UserID, id); err != nil {
			return mutations.Result{}, err
		}
		for _, g := range groupIDs {
			var rev int64
			if err := tx.QueryRow(ctx, `UPDATE groups SET revision = revision + 1 WHERE id = $1 RETURNING revision`, g).Scan(&rev); err != nil {
				return mutations.Result{}, err
			}
			if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityGroup, EntityID: g, EventType: "group_upserted", Revision: rev}); err != nil {
				return mutations.Result{}, err
			}
		}
		if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityPlaylist, EntityID: id, EventType: "playlist_deleted"}); err != nil {
			return mutations.Result{}, err
		}
		return mutations.OK(gin.H{"deleted": true}), nil
	})
}

// AddPlaylistSongs appends synced songs to a playlist. Songs already in it are
// skipped; a playlist never holds the same song twice.
func (h *Handlers) AddPlaylistSongs(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	raw, err := bindStrict(c, "track_ids", "expected_revision")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		ids, err := uuids(raw, "track_ids")
		if err != nil {
			return mutations.Result{}, err
		}
		exp, err := revision(raw)
		if err != nil {
			return mutations.Result{}, err
		}
		var rev int64
		err = tx.QueryRow(ctx, `UPDATE playlists SET revision = revision + 1 WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`, s.UserID, id, exp).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "playlists", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		if err := AppendPlaylistTracks(ctx, tx, s.UserID, id, ids); err != nil {
			return mutations.Result{}, err
		}
		return h.playlistResult(ctx, tx, s.UserID, id, "playlist_upserted", rev)
	})
}

// AppendPlaylistTracks adds tracks at the end of a playlist in the given order.
func AppendPlaylistTracks(ctx context.Context, tx pgx.Tx, userID, playlistID uuid.UUID, trackIDs []uuid.UUID) error {
	var count int
	var last *string
	if err := tx.QueryRow(ctx, `SELECT count(*), max(position COLLATE "C") FROM playlist_items WHERE playlist_id = $1`, playlistID).Scan(&count, &last); err != nil {
		return err
	}
	key := ""
	if last != nil {
		key = *last
	}
	for _, t := range trackIDs {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM playlist_items WHERE playlist_id = $1 AND track_id = $2)`, playlistID, t).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		if count >= MaxSongsPerPlaylist {
			return apperr.WithDetails(apperr.LimitReached, "Playlist song limit reached", map[string]any{"limit": MaxSongsPerPlaylist})
		}
		key = position.After(key)
		_, err := tx.Exec(ctx, `INSERT INTO playlist_items (user_id, playlist_id, track_id, position) VALUES ($1, $2, $3, $4)`, userID, playlistID, t, key)
		if db.IsForeignKeyViolation(err) {
			return apperr.WithDetails(apperr.NotFound, "Song not found", map[string]any{"track_id": t})
		}
		if err != nil {
			return err
		}
		count++
	}
	return nil
}

// RemovePlaylistSongs removes memberships only; it never deletes songs (R-ORG-5).
func (h *Handlers) RemovePlaylistSongs(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	raw, err := bindStrict(c, "track_ids", "expected_revision")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		ids, err := uuids(raw, "track_ids")
		if err != nil {
			return mutations.Result{}, err
		}
		exp, err := revision(raw)
		if err != nil {
			return mutations.Result{}, err
		}
		var rev int64
		err = tx.QueryRow(ctx, `UPDATE playlists SET revision = revision + 1 WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`, s.UserID, id, exp).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "playlists", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM playlist_items WHERE user_id = $1 AND playlist_id = $2 AND track_id = ANY($3)`, s.UserID, id, ids); err != nil {
			return mutations.Result{}, err
		}
		return h.playlistResult(ctx, tx, s.UserID, id, "playlist_upserted", rev)
	})
}

func (h *Handlers) MovePlaylistSong(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req moveRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		var rev int64
		err := tx.QueryRow(ctx, `UPDATE playlists SET revision = revision + 1 WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`, s.UserID, id, req.ExpectedRevision).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "playlists", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		key, err := memberKey(ctx, tx, `SELECT position FROM playlist_items WHERE playlist_id = $1 AND track_id = $2`,
			`SELECT max(position COLLATE "C") FROM playlist_items WHERE playlist_id = $1 AND track_id <> $2`, id, req)
		if err != nil {
			return mutations.Result{}, err
		}
		tag, err := tx.Exec(ctx, `UPDATE playlist_items SET position = $3 WHERE playlist_id = $1 AND track_id = $2`, id, req.ItemID, key)
		if err != nil {
			return mutations.Result{}, err
		}
		if tag.RowsAffected() == 0 {
			return mutations.Result{}, apperr.New(apperr.NotFound, "Song is not in this playlist")
		}
		return h.playlistResult(ctx, tx, s.UserID, id, "playlist_upserted", rev)
	})
}

// memberKey computes a key between the neighbors of a collection member.
func memberKey(ctx context.Context, tx pgx.Tx, posQuery, lastQuery string, collectionID uuid.UUID, req moveRequest) (string, error) {
	neighbor := func(id *uuid.UUID) (*string, error) {
		if id == nil {
			return nil, nil
		}
		var p string
		err := tx.QueryRow(ctx, posQuery, collectionID, *id).Scan(&p)
		if db.IsNoRows(err) {
			return nil, apperr.New(apperr.RevisionConflict, "The order changed on another device. Try again.")
		}
		return &p, err
	}
	after, err := neighbor(req.AfterID)
	if err != nil {
		return "", err
	}
	before, err := neighbor(req.BeforeID)
	if err != nil {
		return "", err
	}
	if after == nil && before == nil {
		if err := tx.QueryRow(ctx, lastQuery, collectionID, req.ItemID).Scan(&after); err != nil {
			return "", err
		}
	}
	return between(after, before)
}

// Groups ----------------------------------------------------------------------

func (h *Handlers) ListGroups(c *gin.Context) {
	s := httpx.CurrentSession(c)
	docs, err := LoadGroups(c.Request.Context(), h.d.Pool, s.UserID, nil)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": docs})
}

func (h *Handlers) CreateGroup(c *gin.Context) {
	raw, err := bindStrict(c, "name")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		name, err := fields.Name("name", str(raw, "name"), fields.MaxGroupName)
		if err != nil {
			return mutations.Result{}, err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM groups WHERE user_id = $1`, s.UserID).Scan(&n); err != nil {
			return mutations.Result{}, err
		}
		if n >= MaxGroups {
			return mutations.Result{}, apperr.WithDetails(apperr.LimitReached, "Group limit reached", map[string]any{"limit": MaxGroups})
		}
		id, _ := uuid.NewV7()
		if _, err := tx.Exec(ctx, `INSERT INTO groups (id, user_id, name) VALUES ($1, $2, $3)`, id, s.UserID, name); err != nil {
			return mutations.Result{}, err
		}
		res, err := h.groupResult(ctx, tx, s.UserID, id, "group_upserted", 1)
		res.Status = http.StatusCreated
		return res, err
	})
}

func (h *Handlers) RenameGroup(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	raw, err := bindStrict(c, "name", "expected_revision")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		name, err := fields.Name("name", str(raw, "name"), fields.MaxGroupName)
		if err != nil {
			return mutations.Result{}, err
		}
		exp, err := revision(raw)
		if err != nil {
			return mutations.Result{}, err
		}
		var rev int64
		err = tx.QueryRow(ctx, `UPDATE groups SET name = $4, revision = revision + 1 WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`, s.UserID, id, exp, name).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "groups", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		return h.groupResult(ctx, tx, s.UserID, id, "group_upserted", rev)
	})
}

// DeleteGroup keeps its playlists (R-ORG-5).
func (h *Handlers) DeleteGroup(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		tag, err := tx.Exec(ctx, `DELETE FROM groups WHERE user_id = $1 AND id = $2`, s.UserID, id)
		if err != nil {
			return mutations.Result{}, err
		}
		if tag.RowsAffected() == 0 {
			return mutations.Result{}, apperr.New(apperr.NotFound, "Group not found")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sync_scopes WHERE user_id = $1 AND scope_type = 'group' AND collection_id = $2`, s.UserID, id); err != nil {
			return mutations.Result{}, err
		}
		if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityGroup, EntityID: id, EventType: "group_deleted"}); err != nil {
			return mutations.Result{}, err
		}
		return mutations.OK(gin.H{"deleted": true}), nil
	})
}

func (h *Handlers) AddGroupPlaylists(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	raw, err := bindStrict(c, "playlist_ids", "expected_revision")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		ids, err := uuids(raw, "playlist_ids")
		if err != nil {
			return mutations.Result{}, err
		}
		exp, err := revision(raw)
		if err != nil {
			return mutations.Result{}, err
		}
		var rev int64
		err = tx.QueryRow(ctx, `UPDATE groups SET revision = revision + 1 WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`, s.UserID, id, exp).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "groups", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		var last *string
		if err := tx.QueryRow(ctx, `SELECT max(position COLLATE "C") FROM group_playlists WHERE group_id = $1`, id).Scan(&last); err != nil {
			return mutations.Result{}, err
		}
		key := ""
		if last != nil {
			key = *last
		}
		for _, p := range ids {
			key = position.After(key)
			_, err := tx.Exec(ctx, `INSERT INTO group_playlists (user_id, group_id, playlist_id, position) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, s.UserID, id, p, key)
			if db.IsForeignKeyViolation(err) {
				return mutations.Result{}, apperr.WithDetails(apperr.NotFound, "Playlist not found", map[string]any{"playlist_id": p})
			}
			if err != nil {
				return mutations.Result{}, err
			}
		}
		return h.groupResult(ctx, tx, s.UserID, id, "group_upserted", rev)
	})
}

func (h *Handlers) RemoveGroupPlaylists(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	raw, err := bindStrict(c, "playlist_ids", "expected_revision")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		ids, err := uuids(raw, "playlist_ids")
		if err != nil {
			return mutations.Result{}, err
		}
		exp, err := revision(raw)
		if err != nil {
			return mutations.Result{}, err
		}
		var rev int64
		err = tx.QueryRow(ctx, `UPDATE groups SET revision = revision + 1 WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`, s.UserID, id, exp).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "groups", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM group_playlists WHERE user_id = $1 AND group_id = $2 AND playlist_id = ANY($3)`, s.UserID, id, ids); err != nil {
			return mutations.Result{}, err
		}
		return h.groupResult(ctx, tx, s.UserID, id, "group_upserted", rev)
	})
}

func (h *Handlers) MoveGroupPlaylist(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req moveRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		var rev int64
		err := tx.QueryRow(ctx, `UPDATE groups SET revision = revision + 1 WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`, s.UserID, id, req.ExpectedRevision).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "groups", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		key, err := memberKey(ctx, tx, `SELECT position FROM group_playlists WHERE group_id = $1 AND playlist_id = $2`,
			`SELECT max(position COLLATE "C") FROM group_playlists WHERE group_id = $1 AND playlist_id <> $2`, id, req)
		if err != nil {
			return mutations.Result{}, err
		}
		tag, err := tx.Exec(ctx, `UPDATE group_playlists SET position = $3 WHERE group_id = $1 AND playlist_id = $2`, id, req.ItemID, key)
		if err != nil {
			return mutations.Result{}, err
		}
		if tag.RowsAffected() == 0 {
			return mutations.Result{}, apperr.New(apperr.NotFound, "Playlist is not in this group")
		}
		return h.groupResult(ctx, tx, s.UserID, id, "group_upserted", rev)
	})
}

// Sync scopes -----------------------------------------------------------------

func (h *Handlers) ListScopes(c *gin.Context) {
	s := httpx.CurrentSession(c)
	docs, err := LoadScopes(c.Request.Context(), h.d.Pool, s.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"scopes": docs})
}

// SetScope enables (PUT) or pauses (POST …/pause) a sync scope (R-CLOUD-1, R-CLOUD-7).
func (h *Handlers) SetScope(state string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scopeType := c.Param("type")
		if scopeType != "library" && scopeType != "playlist" && scopeType != "group" {
			httpx.Fail(c, apperr.New(apperr.NotFound, "Unknown scope type"))
			return
		}
		coll := uuid.Nil
		if scopeType != "library" {
			id, err := httpx.ParamUUID(c, "id")
			if err != nil {
				httpx.Fail(c, err)
				return
			}
			coll = id
		}
		mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
			if scopeType != "library" {
				table := "playlists"
				if scopeType == "group" {
					table = "groups"
				}
				var exists bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE user_id = $1 AND id = $2)`, s.UserID, coll).Scan(&exists); err != nil {
					return mutations.Result{}, err
				}
				if !exists {
					return mutations.Result{}, apperr.New(apperr.NotFound, "Not found")
				}
			}
			id, _ := uuid.NewV7()
			var scopeID uuid.UUID
			var rev int64
			err := tx.QueryRow(ctx, `
				INSERT INTO sync_scopes (id, user_id, scope_type, collection_id, state) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (user_id, scope_type, collection_id) DO UPDATE SET state = EXCLUDED.state, revision = sync_scopes.revision + 1
				RETURNING id, revision`, id, s.UserID, scopeType, coll, state).Scan(&scopeID, &rev)
			if err != nil {
				return mutations.Result{}, err
			}
			if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityScope, EntityID: scopeID, EventType: "scope_changed", Revision: rev}); err != nil {
				return mutations.Result{}, err
			}
			scopes, err := LoadScopes(ctx, tx, s.UserID)
			if err != nil {
				return mutations.Result{}, err
			}
			return mutations.OK(gin.H{"scopes": scopes}), nil
		})
	}
}
