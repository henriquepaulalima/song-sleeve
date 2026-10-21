package catalog

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/fields"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
	"github.com/henriquepaulalima/song-sleeve/server/internal/mutations"
	"github.com/henriquepaulalima/song-sleeve/server/pkg/position"
)

// Scale caps (D-33).
const (
	MaxSyncedSongs      = 10000
	MaxPlaylists        = 500
	MaxGroups           = 100
	MaxSongsPerPlaylist = 5000
)

type Handlers struct{ d *app.Deps }

func New(d *app.Deps) *Handlers { return &Handlers{d: d} }

var revisionTables = map[string]bool{"artists": true, "tracks": true, "playlists": true, "groups": true}

// revisionOrMissing explains why a revision-guarded UPDATE matched nothing: the row
// is gone (not_found) or the client's revision is stale (revision_conflict, R-MUT-3).
func (h *Handlers) revisionOrMissing(ctx context.Context, q db.Querier, table string, userID, id uuid.UUID) error {
	if !revisionTables[table] {
		panic("unknown table " + table)
	}
	var rev int64
	err := q.QueryRow(ctx, `SELECT revision FROM `+table+` WHERE user_id = $1 AND id = $2`, userID, id).Scan(&rev)
	if db.IsNoRows(err) {
		return apperr.New(apperr.NotFound, "Not found")
	}
	if err != nil {
		return err
	}
	return apperr.WithDetails(apperr.RevisionConflict, "This changed on another device. Review it and try again.", map[string]any{"current_revision": rev})
}

type patchTrackRequest struct {
	ExpectedRevision int64 `json:"expected_revision"`
	Name             *string
	// Artist is tri-state: absent = unchanged, null = clear, object = set.
	Artist            *ArtistRef `json:"-"`
	ArtistSet         bool       `json:"-"`
	ResolveSuggestion bool       `json:"resolve_suggestion"`
}

// PatchTrack edits a synced song's name and artist (§7.5). Files and content never
// change, and the edit never takes the operation slot (R-SYNC-1).
func (h *Handlers) PatchTrack(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		httpx.Fail(c, apperr.New(apperr.InvalidRequest, "Invalid request body"))
		return
	}
	req, err := parsePatchTrack(raw)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		var name *string
		if req.Name != nil {
			n, err := fields.Name("name", *req.Name, fields.MaxSongName)
			if err != nil {
				return mutations.Result{}, err
			}
			name = &n
		}
		var artistID *uuid.UUID
		if req.ArtistSet {
			if artistID, err = ResolveArtist(ctx, tx, s.UserID, req.Artist); err != nil {
				return mutations.Result{}, err
			}
		}
		var rev int64
		err := tx.QueryRow(ctx, `
			UPDATE tracks SET
				name = COALESCE($4, name),
				artist_id = CASE WHEN $5 THEN $6 ELSE artist_id END,
				metadata_suggestion = CASE WHEN $7 THEN NULL ELSE metadata_suggestion END,
				revision = revision + 1
			WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`,
			s.UserID, id, req.ExpectedRevision, name, req.ArtistSet, artistID, req.ResolveSuggestion).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "tracks", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityTrack, EntityID: id, EventType: "track_upserted", Revision: rev}); err != nil {
			return mutations.Result{}, err
		}
		t, err := LoadTrack(ctx, tx, s.UserID, id)
		if err != nil {
			return mutations.Result{}, err
		}
		return mutations.OK(gin.H{"track": t}), nil
	})
}

func parsePatchTrack(raw map[string]any) (*patchTrackRequest, error) {
	req := &patchTrackRequest{}
	rev, ok := raw["expected_revision"].(float64)
	if !ok {
		return nil, apperr.Field("expected_revision", "required")
	}
	req.ExpectedRevision = int64(rev)
	if v, ok := raw["name"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, apperr.Field("name", "invalid")
		}
		req.Name = &s
	}
	if v, ok := raw["artist"]; ok {
		req.ArtistSet = true
		if v != nil {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, apperr.Field("artist", "invalid")
			}
			ref := &ArtistRef{}
			if idStr, ok := m["id"].(string); ok {
				id, err := uuid.Parse(idStr)
				if err != nil {
					return nil, apperr.Field("artist.id", "invalid")
				}
				ref.ID = &id
			}
			if n, ok := m["name"].(string); ok {
				ref.Name = &n
			}
			if ref.ID == nil && ref.Name == nil {
				return nil, apperr.Field("artist", "invalid")
			}
			req.Artist = ref
		}
	}
	if v, ok := raw["resolve_suggestion"].(bool); ok {
		req.ResolveSuggestion = v
	}
	return req, nil
}

// moveRequest places an item between two neighbors; nil neighbors mean the start
// or the end (Move to top/bottom).
type moveRequest struct {
	ItemID           uuid.UUID  `json:"item_id"`
	AfterID          *uuid.UUID `json:"after_id"`
	BeforeID         *uuid.UUID `json:"before_id"`
	ExpectedRevision int64      `json:"expected_revision"`
}

// between computes the new key from the neighbors' current positions. If the
// neighbors are no longer adjacent in order the result is still a valid key
// between them, which is what the user dragged to.
func between(after, before *string) (string, error) {
	a, b := "", ""
	if after != nil {
		a = *after
	}
	if before != nil {
		b = *before
	}
	if a != "" && b != "" && a >= b {
		return "", apperr.New(apperr.RevisionConflict, "The order changed on another device. Try again.")
	}
	k, err := position.Between(a, b)
	if err != nil {
		return "", apperr.New(apperr.RevisionConflict, "The order changed on another device. Try again.")
	}
	return k, nil
}

// MoveLibrarySong reorders one song in the library order (R-ORG-6): one row write.
func (h *Handlers) MoveLibrarySong(c *gin.Context) {
	var req moveRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		neighbor := func(id *uuid.UUID) (*string, error) {
			if id == nil {
				return nil, nil
			}
			var p string
			err := tx.QueryRow(ctx, `SELECT library_position FROM tracks WHERE user_id = $1 AND id = $2`, s.UserID, *id).Scan(&p)
			if db.IsNoRows(err) {
				return nil, apperr.New(apperr.RevisionConflict, "The order changed on another device. Try again.")
			}
			return &p, err
		}
		after, err := neighbor(req.AfterID)
		if err != nil {
			return mutations.Result{}, err
		}
		before, err := neighbor(req.BeforeID)
		if err != nil {
			return mutations.Result{}, err
		}
		if after == nil && before == nil {
			// Move to bottom when no neighbor is given.
			var last *string
			if err := tx.QueryRow(ctx, `SELECT max(library_position COLLATE "C") FROM tracks WHERE user_id = $1 AND id <> $2`, s.UserID, req.ItemID).Scan(&last); err != nil {
				return mutations.Result{}, err
			}
			after = last
		}
		key, err := between(after, before)
		if err != nil {
			return mutations.Result{}, err
		}
		var rev int64
		err = tx.QueryRow(ctx, `UPDATE tracks SET library_position = $4, revision = revision + 1 WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`,
			s.UserID, req.ItemID, req.ExpectedRevision, key).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "tracks", s.UserID, req.ItemID)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityTrack, EntityID: req.ItemID, EventType: "track_upserted", Revision: rev}); err != nil {
			return mutations.Result{}, err
		}
		t, err := LoadTrack(ctx, tx, s.UserID, req.ItemID)
		if err != nil {
			return mutations.Result{}, err
		}
		return mutations.OK(gin.H{"track": t}), nil
	})
}

// NextLibraryPosition appends at the end of the library order.
func NextLibraryPosition(ctx context.Context, q db.Querier, userID uuid.UUID) (string, error) {
	var last *string
	if err := q.QueryRow(ctx, `SELECT max(library_position COLLATE "C") FROM tracks WHERE user_id = $1`, userID).Scan(&last); err != nil {
		return "", err
	}
	if last == nil {
		return position.After(""), nil
	}
	return position.After(*last), nil
}

func (h *Handlers) GetPreferences(c *gin.Context) {
	s := httpx.CurrentSession(c)
	p, err := LoadPreferences(c.Request.Context(), h.d.Pool, s.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

type preferencesRequest struct {
	Songs            bool  `json:"songs"`
	Playlists        bool  `json:"playlists"`
	Groups           bool  `json:"groups"`
	ExpectedRevision int64 `json:"expected_revision"`
}

// PutPreferences saves the Library type filter; at least one type stays on (R-ORG-7).
func (h *Handlers) PutPreferences(c *gin.Context) {
	var req preferencesRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if !req.Songs && !req.Playlists && !req.Groups {
		httpx.Fail(c, apperr.Field("preferences", "at_least_one"))
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		var rev int64
		err := tx.QueryRow(ctx, `
			UPDATE library_view_preferences SET songs = $3, playlists = $4, groups = $5, revision = revision + 1
			WHERE user_id = $1 AND revision = $2 RETURNING revision`,
			s.UserID, req.ExpectedRevision, req.Songs, req.Playlists, req.Groups).Scan(&rev)
		if db.IsNoRows(err) {
			return mutations.Result{}, apperr.New(apperr.RevisionConflict, "This changed on another device.")
		}
		if err != nil {
			return mutations.Result{}, err
		}
		if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityPreferences, EntityID: s.UserID, EventType: "preferences_changed", Revision: rev}); err != nil {
			return mutations.Result{}, err
		}
		p, err := LoadPreferences(ctx, tx, s.UserID)
		if err != nil {
			return mutations.Result{}, err
		}
		return mutations.OK(gin.H{"preferences": p}), nil
	})
}
