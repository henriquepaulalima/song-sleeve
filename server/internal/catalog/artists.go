package catalog

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/fields"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
	"github.com/henriquepaulalima/song-sleeve/server/internal/mutations"
)

// MaxArtists is not a plan cap but keeps the client-side matcher's list bounded.
const MaxArtists = 10000

// ArtistRef picks an artist in a request: an existing ID, or a name that is reused
// by normalized name or created (R-ORG-2). A nil ref means "no artist".
type ArtistRef struct {
	ID   *uuid.UUID `json:"id"`
	Name *string    `json:"name"`
}

// ResolveArtist returns the artist ID for ref, creating the artist inside tx when
// needed. Creation happens only because the user confirmed it.
func ResolveArtist(ctx context.Context, tx pgx.Tx, userID uuid.UUID, ref *ArtistRef) (*uuid.UUID, error) {
	if ref == nil {
		return nil, nil
	}
	if ref.ID != nil {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM artists WHERE user_id = $1 AND id = $2`, userID, *ref.ID).Scan(&id)
		if db.IsNoRows(err) {
			if ref.Name == nil {
				return nil, apperr.WithDetails(apperr.NotFound, "Artist not found", map[string]any{"entity": "artist"})
			}
			// The cached artist ID is stale (F-10): fall back to the name.
		} else if err != nil {
			return nil, err
		} else {
			return &id, nil
		}
	}
	if ref.Name == nil {
		return nil, nil
	}
	name, err := fields.Name("artist", *ref.Name, fields.MaxArtistName)
	if err != nil {
		return nil, err
	}
	norm := fields.NormalizeArtist(name)
	var id uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM artists WHERE user_id = $1 AND normalized_name = $2`, userID, norm).Scan(&id)
	if err == nil {
		return &id, nil
	}
	if !db.IsNoRows(err) {
		return nil, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM artists WHERE user_id = $1`, userID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= MaxArtists {
		return nil, apperr.WithDetails(apperr.LimitReached, "Artist limit reached", map[string]any{"limit": MaxArtists})
	}
	id, _ = uuid.NewV7()
	// ON CONFLICT covers a concurrent create of the same normalized name.
	err = tx.QueryRow(ctx, `
		INSERT INTO artists (id, user_id, name, normalized_name) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, normalized_name) DO UPDATE SET normalized_name = EXCLUDED.normalized_name
		RETURNING id`, id, userID, name, norm).Scan(&id)
	if err != nil {
		return nil, err
	}
	if _, err := events.Append(ctx, tx, events.Event{UserID: userID, EntityType: events.EntityArtist, EntityID: id, EventType: "artist_upserted", Revision: 1}); err != nil {
		return nil, err
	}
	return &id, nil
}

type renameRequest struct {
	Name             string `json:"name"`
	ExpectedRevision int64  `json:"expected_revision"`
}

func (h *Handlers) RenameArtist(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req renameRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		name, err := fields.Name("name", req.Name, fields.MaxArtistName)
		if err != nil {
			return mutations.Result{}, err
		}
		var rev int64
		err = tx.QueryRow(ctx, `
			UPDATE artists SET name = $4, normalized_name = $5, revision = revision + 1
			WHERE user_id = $1 AND id = $2 AND revision = $3 RETURNING revision`,
			s.UserID, id, req.ExpectedRevision, name, fields.NormalizeArtist(name)).Scan(&rev)
		if db.IsUniqueViolation(err, "artists_user_id_normalized_name_key") {
			return mutations.Result{}, apperr.Field("name", "artist_exists")
		}
		if db.IsNoRows(err) {
			return mutations.Result{}, h.revisionOrMissing(ctx, tx, "artists", s.UserID, id)
		}
		if err != nil {
			return mutations.Result{}, err
		}
		if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityArtist, EntityID: id, EventType: "artist_upserted", Revision: rev}); err != nil {
			return mutations.Result{}, err
		}
		docs, err := LoadArtists(ctx, tx, s.UserID, []uuid.UUID{id})
		if err != nil {
			return mutations.Result{}, err
		}
		return mutations.OK(gin.H{"artist": docs[0]}), nil
	})
}

// DeleteArtist is allowed only while no synced song uses the artist.
func (h *Handlers) DeleteArtist(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tracks WHERE user_id = $1 AND artist_id = $2)`, s.UserID, id).Scan(&used); err != nil {
			return mutations.Result{}, err
		}
		if used {
			return mutations.Result{}, apperr.New(apperr.ArtistInUse, "Delete is available once no song uses this artist.")
		}
		tag, err := tx.Exec(ctx, `DELETE FROM artists WHERE user_id = $1 AND id = $2`, s.UserID, id)
		if err != nil {
			return mutations.Result{}, err
		}
		if tag.RowsAffected() == 0 {
			return mutations.Result{}, apperr.New(apperr.NotFound, "Artist not found")
		}
		if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityArtist, EntityID: id, EventType: "artist_deleted"}); err != nil {
			return mutations.Result{}, err
		}
		return mutations.OK(gin.H{"deleted": true}), nil
	})
}

func (h *Handlers) ListArtists(c *gin.Context) {
	s := httpx.CurrentSession(c)
	all, err := LoadArtists(c.Request.Context(), h.d.Pool, s.UserID, nil)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	if q != "" {
		filtered := all[:0]
		for _, a := range all {
			if strings.Contains(strings.ToLower(a.Name), q) {
				filtered = append(filtered, a)
			}
		}
		all = filtered
	}
	c.JSON(http.StatusOK, gin.H{"artists": all})
}

func (h *Handlers) ArtistSongs(c *gin.Context) {
	s := httpx.CurrentSession(c)
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows, err := h.d.Pool.Query(c.Request.Context(), `SELECT id FROM tracks WHERE user_id = $1 AND artist_id = $2 ORDER BY library_position, id`, s.UserID, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	tracks, err := LoadTracks(c.Request.Context(), h.d.Pool, s.UserID, ids)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tracks": tracks})
}
