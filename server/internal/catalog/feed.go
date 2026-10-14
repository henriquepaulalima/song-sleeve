package catalog

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
)

const snapshotPageSize = 1000

type pageToken struct {
	Pos string    `json:"p"`
	ID  uuid.UUID `json:"i"`
}

func encodeToken(t *Track) string {
	raw, _ := json.Marshal(pageToken{Pos: t.LibraryPosition, ID: t.ID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeToken(s string) (pageToken, error) {
	var t pageToken
	if s == "" {
		return t, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(raw, &t) != nil {
		return t, apperr.Field("page_token", "invalid")
	}
	return t, nil
}

// Snapshot serves GET /me/catalog/snapshot. The first page carries the cursor the
// client resumes the change feed from, plus all organization; later pages carry
// tracks only (§4.4 snapshot paging).
func (h *Handlers) Snapshot(c *gin.Context) {
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	tok, err := decodeToken(c.Query("page_token"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	resp := gin.H{}
	// A repeatable-read transaction gives the first page one consistent view.
	err = db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
			return err
		}
		tracks, err := TrackPage(ctx, tx, s.UserID, tok.Pos, tok.ID, "", snapshotPageSize)
		if err != nil {
			return err
		}
		resp["tracks"] = tracks
		if len(tracks) == snapshotPageSize {
			resp["next_page_token"] = encodeToken(tracks[len(tracks)-1])
		}
		if tok.Pos != "" {
			return nil
		}
		cursor, err := events.LatestCursor(ctx, tx, s.UserID)
		if err != nil {
			return err
		}
		resp["cursor"] = cursor
		if resp["artists"], err = LoadArtists(ctx, tx, s.UserID, nil); err != nil {
			return err
		}
		if resp["playlists"], err = LoadPlaylists(ctx, tx, s.UserID, nil); err != nil {
			return err
		}
		if resp["groups"], err = LoadGroups(ctx, tx, s.UserID, nil); err != nil {
			return err
		}
		if resp["preferences"], err = LoadPreferences(ctx, tx, s.UserID); err != nil {
			return err
		}
		resp["scopes"], err = LoadScopes(ctx, tx, s.UserID)
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

type changeEvent struct {
	Cursor     int64          `json:"cursor"`
	EntityType string         `json:"entity_type"`
	EntityID   uuid.UUID      `json:"entity_id"`
	EventType  string         `json:"event_type"`
	Revision   int64          `json:"revision"`
	Payload    map[string]any `json:"payload,omitempty"`
}

// Changes serves GET /me/catalog/changes?cursor=N: the events after N and the
// current documents they refer to. A referenced entity that no longer exists is
// reported as deleted. An expired cursor returns cursor_expired (R-EVT-4).
func (h *Handlers) Changes(c *gin.Context) {
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	cursor, err := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	if err != nil || cursor < 0 {
		httpx.Fail(c, apperr.Field("cursor", "invalid"))
		return
	}
	var purged int64
	if err := h.d.Pool.QueryRow(ctx, `SELECT events_purged_through FROM users WHERE id = $1`, s.UserID).Scan(&purged); err != nil {
		httpx.Fail(c, err)
		return
	}
	if cursor < purged {
		httpx.Fail(c, apperr.New(apperr.CursorExpired, "Catalog cursor expired; load a full snapshot."))
		return
	}
	const limit = 500
	rows, err := h.d.Pool.Query(ctx, `
		SELECT cursor, entity_type, entity_id, event_type, revision, payload FROM account_events
		WHERE user_id = $1 AND cursor > $2 ORDER BY cursor LIMIT $3`, s.UserID, cursor, limit+1)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	evs := []changeEvent{}
	for rows.Next() {
		var e changeEvent
		if err := rows.Scan(&e.Cursor, &e.EntityType, &e.EntityID, &e.EventType, &e.Revision, &e.Payload); err != nil {
			rows.Close()
			httpx.Fail(c, err)
			return
		}
		evs = append(evs, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		httpx.Fail(c, err)
		return
	}
	hasMore := len(evs) > limit
	if hasMore {
		evs = evs[:limit]
	}
	next := cursor
	ids := map[string][]uuid.UUID{}
	seen := map[string]map[uuid.UUID]bool{}
	scopes, prefs, account := false, false, false
	for _, e := range evs {
		next = e.Cursor
		switch e.EntityType {
		case events.EntityScope:
			scopes = true
		case events.EntityPreferences:
			prefs = true
		case events.EntityAccount:
			account = true
		default:
			if seen[e.EntityType] == nil {
				seen[e.EntityType] = map[uuid.UUID]bool{}
			}
			if !seen[e.EntityType][e.EntityID] {
				seen[e.EntityType][e.EntityID] = true
				ids[e.EntityType] = append(ids[e.EntityType], e.EntityID)
			}
		}
	}
	resp := gin.H{"cursor": next, "has_more": hasMore, "events": evs}
	deleted := map[string][]uuid.UUID{}
	found := func(kind string, present map[uuid.UUID]bool) {
		for _, id := range ids[kind] {
			if !present[id] {
				deleted[kind] = append(deleted[kind], id)
			}
		}
	}
	if len(ids[events.EntityTrack]) > 0 {
		docs, err := LoadTracks(ctx, h.d.Pool, s.UserID, ids[events.EntityTrack])
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		present := map[uuid.UUID]bool{}
		for _, d := range docs {
			present[d.ID] = true
		}
		found(events.EntityTrack, present)
		resp["tracks"] = docs
	}
	if len(ids[events.EntityArtist]) > 0 {
		docs, err := LoadArtists(ctx, h.d.Pool, s.UserID, ids[events.EntityArtist])
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		present := map[uuid.UUID]bool{}
		for _, d := range docs {
			present[d.ID] = true
		}
		found(events.EntityArtist, present)
		resp["artists"] = docs
	}
	if len(ids[events.EntityPlaylist]) > 0 {
		docs, err := LoadPlaylists(ctx, h.d.Pool, s.UserID, ids[events.EntityPlaylist])
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		present := map[uuid.UUID]bool{}
		for _, d := range docs {
			present[d.ID] = true
		}
		found(events.EntityPlaylist, present)
		resp["playlists"] = docs
	}
	if len(ids[events.EntityGroup]) > 0 {
		docs, err := LoadGroups(ctx, h.d.Pool, s.UserID, ids[events.EntityGroup])
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		present := map[uuid.UUID]bool{}
		for _, d := range docs {
			present[d.ID] = true
		}
		found(events.EntityGroup, present)
		resp["groups"] = docs
	}
	if scopes {
		if resp["scopes"], err = LoadScopes(ctx, h.d.Pool, s.UserID); err != nil {
			httpx.Fail(c, err)
			return
		}
	}
	if prefs {
		if resp["preferences"], err = LoadPreferences(ctx, h.d.Pool, s.UserID); err != nil {
			httpx.Fail(c, err)
			return
		}
	}
	resp["account_changed"] = account
	resp["deleted"] = deleted
	c.JSON(http.StatusOK, resp)
}

// ListTracks serves GET /me/tracks?cursor&q, paginated in library order.
func (h *Handlers) ListTracks(c *gin.Context) {
	s := httpx.CurrentSession(c)
	tok, err := decodeToken(c.Query("cursor"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	q = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	limit := 200
	tracks, err := TrackPage(c.Request.Context(), h.d.Pool, s.UserID, tok.Pos, tok.ID, q, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	resp := gin.H{"tracks": tracks}
	if len(tracks) == limit {
		resp["next_cursor"] = encodeToken(tracks[len(tracks)-1])
	}
	c.JSON(http.StatusOK, resp)
}

// QueueIDs returns the ordered track IDs of a whole list in one response, so the
// web can snapshot a queue while its lists stay paginated (F-13).
func (h *Handlers) QueueIDs(c *gin.Context) {
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	source := c.Query("source")
	var rows pgx.Rows
	var err error
	switch {
	case source == "library":
		rows, err = h.d.Pool.Query(ctx, `SELECT id FROM tracks WHERE user_id = $1 ORDER BY library_position, id`, s.UserID)
	case strings.HasPrefix(source, "playlist:"):
		id, perr := uuid.Parse(strings.TrimPrefix(source, "playlist:"))
		if perr != nil {
			httpx.Fail(c, apperr.Field("source", "invalid"))
			return
		}
		rows, err = h.d.Pool.Query(ctx, `
			SELECT i.track_id FROM playlist_items i JOIN playlists p ON p.id = i.playlist_id
			WHERE p.user_id = $1 AND p.id = $2 ORDER BY i.position, i.track_id`, s.UserID, id)
	case strings.HasPrefix(source, "artist:"):
		id, perr := uuid.Parse(strings.TrimPrefix(source, "artist:"))
		if perr != nil {
			httpx.Fail(c, apperr.Field("source", "invalid"))
			return
		}
		rows, err = h.d.Pool.Query(ctx, `SELECT id FROM tracks WHERE user_id = $1 AND artist_id = $2 ORDER BY library_position, id`, s.UserID, id)
	default:
		httpx.Fail(c, apperr.Field("source", "invalid"))
		return
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"track_ids": ids})
}
