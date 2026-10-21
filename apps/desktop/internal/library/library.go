// Package library is the installed apps' local-song service (§7.1, R-LOCAL): import
// by recording where a file already is, offline edits, relink and health checks,
// the playlist overlay, and the merged library order of synced and local songs.
// It never modifies, moves or deletes a source file (R-LOCAL-4).
package library

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/accountdb"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/probe"
	"github.com/henriquepaulalima/song-sleeve/server/pkg/position"
)

const (
	MaxDurationMs = 1800 * 1000
	MaxFileBytes  = 134217728
	MaxSongName   = 200
	MaxArtistName = 200
	// MaxLocalSongs is the scale target for one device (D-33).
	MaxLocalSongs = 10000
)

var (
	// ErrInSongSleeveFolder: "This file is in the Song Sleeve folder, which only holds synced songs."
	ErrInSongSleeveFolder = errors.New("in_song_sleeve_folder")
	ErrNotFound           = errors.New("not_found")
	ErrDifferentFile      = errors.New("different_file")
	ErrInvalidName        = errors.New("invalid_field")
	ErrLimitReached       = errors.New("limit_reached")
)

type Library struct {
	db *accountdb.DB
	// songSleeveRoot is the whole user-visible Song Sleeve folder (every account).
	songSleeveRoot string
	now            func() time.Time
}

func New(db *accountdb.DB, songSleeveRoot string) *Library {
	return &Library{db: db, songSleeveRoot: songSleeveRoot, now: time.Now}
}

// Song is a local song on this device.
type Song struct {
	ID              string    `json:"id"`
	AddedAt         time.Time `json:"added_at"`
	Name            string    `json:"name"`
	ArtistName      *string   `json:"artist_name"`
	ArtistID        *string   `json:"artist_id"`
	DurationMs      int64     `json:"duration_ms"`
	ByteSize        int64     `json:"byte_size"`
	Format          string    `json:"format"`
	SHA256          string    `json:"sha256"`
	SourcePath      string    `json:"source_path"`
	Health          string    `json:"health"`
	LibraryPosition string    `json:"library_position"`
	SyncState       string    `json:"sync_state"`
	SyncFailure     *string   `json:"sync_failure"`
	SyncingTrackID  *string   `json:"syncing_track_id"`
}

// Candidate is one file reviewed in the Add songs dialog before anything is saved.
type Candidate struct {
	Path       string `json:"path"`
	Name       string `json:"name"`        // embedded title or the filename without its extension
	ArtistHint string `json:"artist_hint"` // embedded artist or the part after a hyphen
	Format     string `json:"format"`
	DurationMs int64  `json:"duration_ms"`
	ByteSize   int64  `json:"byte_size"`
	SHA256     string `json:"sha256"`
	// Problem is set when the file can't be added: unsupported_format,
	// song_too_long, song_too_large, invalid_audio_metadata, in_song_sleeve_folder,
	// unreadable.
	Problem string `json:"problem,omitempty"`
}

// CleanName trims, NFC-normalizes and strips control characters (F-20).
func CleanName(s string) string {
	s = norm.NFC.String(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func validName(s string, max int) (string, error) {
	c := CleanName(s)
	if c == "" || utf8.RuneCountInString(c) > max {
		return "", ErrInvalidName
	}
	return c, nil
}

// NormalizeArtist matches the server's artist identity key (case and spacing).
func NormalizeArtist(name string) string {
	s := strings.ToLower(norm.NFKC.String(CleanName(name)))
	return strings.Join(strings.Fields(s), " ")
}

// InsideSongSleeve reports whether path is in the Song Sleeve folder, following
// symlinks so a link can't smuggle a mirror in as a local song.
func (l *Library) InsideSongSleeve(path string) bool {
	root, err := filepath.EvalSymlinks(l.songSleeveRoot)
	if err != nil {
		root = l.songSleeveRoot
	}
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		p = path
	}
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Analyze reads the given files and folders (recursively) for the review list.
// Only supported, readable audio is kept; everything else is reported.
func (l *Library) Analyze(ctx context.Context, paths []string) (candidates []Candidate, skipped []string, err error) {
	var files []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			skipped = append(skipped, p)
			continue
		}
		if !st.IsDir() {
			files = append(files, p)
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != p && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				if l.InsideSongSleeve(path) {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type().IsRegular() {
				files = append(files, path)
			}
			return nil
		})
	}
	for _, f := range files {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		c := l.analyzeFile(f)
		notAudio := c.Problem == "invalid_audio_metadata" && !audioExt[strings.ToLower(filepath.Ext(f))]
		if c.Problem == "unsupported_format" || c.Problem == "unreadable" || notAudio {
			skipped = append(skipped, f)
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates, skipped, nil
}

// audioExt marks files that look like audio, so a broken one is flagged in the
// review list instead of silently skipped like a cover image or a text file.
var audioExt = map[string]bool{".mp3": true, ".m4a": true, ".aac": true, ".mp4": true, ".flac": true, ".wav": true, ".wave": true}

func (l *Library) analyzeFile(path string) Candidate {
	abs, _ := filepath.Abs(path)
	c := Candidate{Path: abs}
	base := filepath.Base(abs)
	c.Name = strings.TrimSuffix(base, filepath.Ext(base))
	if l.InsideSongSleeve(abs) {
		c.Problem = ErrInSongSleeveFolder.Error()
		return c
	}
	facts, err := probe.File(abs)
	switch {
	case errors.Is(err, probe.ErrUnsupported):
		c.Problem = "unsupported_format"
		return c
	case errors.Is(err, probe.ErrInvalid):
		c.Problem = "invalid_audio_metadata"
		return c
	case err != nil:
		c.Problem = "unreadable"
		return c
	}
	c.Format, c.DurationMs, c.ByteSize, c.SHA256 = facts.Format, facts.DurationMs, facts.Bytes, facts.SHA256
	if t := CleanName(facts.Title); t != "" {
		c.Name = t
	}
	if a := CleanName(facts.Artist); a != "" {
		c.ArtistHint = a
	} else if i := strings.Index(c.Name, "-"); i >= 0 {
		c.ArtistHint = strings.TrimSpace(c.Name[i+1:])
	}
	c.Problem = limitProblem(c.DurationMs, c.ByteSize)
	return c
}

func limitProblem(ms, size int64) string {
	switch {
	case ms <= 0:
		return "invalid_audio_metadata"
	case ms > MaxDurationMs:
		return "song_too_long"
	case size > MaxFileBytes:
		return "song_too_large"
	}
	return ""
}

// ImportItem is a reviewed candidate with the name and artist the user chose.
type ImportItem struct {
	Candidate
	Name   string  `json:"name"`
	Artist *string `json:"artist"` // nil = no artist
}

// Import saves reviewed songs as local songs: only a locator to the file is kept,
// nothing is copied, no connection is needed (R-LOCAL-1). Songs go to the end of
// the library order, and optionally into the overlay of each given playlist.
func (l *Library) Import(ctx context.Context, items []ImportItem, playlistIDs []string) ([]Song, error) {
	var ids []string
	err := l.db.InTx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM local_songs`).Scan(&count); err != nil {
			return err
		}
		if count+len(items) > MaxLocalSongs {
			return ErrLimitReached
		}
		pos, err := lastLibraryPosition(ctx, tx)
		if err != nil {
			return err
		}
		for _, it := range items {
			// Re-check every layer's limits and the folder rule at save time (R-LIM-2).
			if l.InsideSongSleeve(it.Path) {
				return ErrInSongSleeveFolder
			}
			if p := limitProblem(it.DurationMs, it.ByteSize); p != "" {
				return errors.New(p)
			}
			name, err := validName(it.Name, MaxSongName)
			if err != nil {
				return err
			}
			artistName, artistID, err := resolveArtist(ctx, tx, it.Artist)
			if err != nil {
				return err
			}
			st, err := os.Stat(it.Path)
			if err != nil {
				return err
			}
			pos = position.After(pos)
			id, _ := uuid.NewV7()
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO local_songs (id, added_at, name, artist_name, artist_id, duration_ms, byte_size, format, sha256,
					source_path, observed_size, observed_mtime, library_position)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id.String(), l.now().UTC().Format(time.RFC3339Nano), name, artistName, artistID, it.DurationMs, it.ByteSize, it.Format, it.SHA256,
				it.Path, st.Size(), st.ModTime().UnixNano(), pos); err != nil {
				return err
			}
			ids = append(ids, id.String())
			for _, pid := range playlistIDs {
				if pid == "" {
					continue
				}
				if err := addOverlay(ctx, tx, l.db, pid, id.String()); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]Song, 0, len(ids))
	for _, id := range ids {
		s, err := l.Song(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, nil
}

// resolveArtist stores the name and links an existing server artist with the same
// normalized name. A new name creates its server artist only when the song syncs (R-ORG-2).
func resolveArtist(ctx context.Context, tx *sql.Tx, artist *string) (*string, *string, error) {
	if artist == nil {
		return nil, nil, nil
	}
	name, err := validName(*artist, MaxArtistName)
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM replica_artists`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	key := NormalizeArtist(name)
	for rows.Next() {
		var id, n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, nil, err
		}
		if NormalizeArtist(n) == key {
			return &n, &id, nil
		}
	}
	return &name, nil, rows.Err()
}

// lastLibraryPosition is the greatest key across synced and local songs: both share
// one key space (R-ORG-6).
func lastLibraryPosition(ctx context.Context, tx *sql.Tx) (string, error) {
	var a, b sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT max(library_position) FROM replica_tracks`).Scan(&a); err != nil {
		return "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT max(library_position) FROM local_songs`).Scan(&b); err != nil {
		return "", err
	}
	if b.String > a.String {
		return b.String, nil
	}
	return a.String, nil
}

const songColumns = `id, added_at, name, artist_name, artist_id, duration_ms, byte_size, format, sha256, source_path,
	health, library_position, sync_state, sync_failure, syncing_track_id`

type scanner interface{ Scan(...any) error }

func scanSong(r scanner) (*Song, error) {
	var s Song
	var added string
	if err := r.Scan(&s.ID, &added, &s.Name, &s.ArtistName, &s.ArtistID, &s.DurationMs, &s.ByteSize, &s.Format, &s.SHA256,
		&s.SourcePath, &s.Health, &s.LibraryPosition, &s.SyncState, &s.SyncFailure, &s.SyncingTrackID); err != nil {
		return nil, err
	}
	s.AddedAt, _ = time.Parse(time.RFC3339Nano, added)
	return &s, nil
}

func (l *Library) Song(ctx context.Context, id string) (*Song, error) {
	s, err := scanSong(l.db.QueryRowContext(ctx, `SELECT `+songColumns+` FROM local_songs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// Songs returns every local song in library order.
func (l *Library) Songs(ctx context.Context) ([]Song, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT `+songColumns+` FROM local_songs ORDER BY library_position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Song
	for rows.Next() {
		s, err := scanSong(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// Edit renames a local song or changes its artist, on the device and offline (R-LOCAL-2).
func (l *Library) Edit(ctx context.Context, id string, name *string, artistSet bool, artist *string) (*Song, error) {
	err := l.db.InTx(ctx, func(tx *sql.Tx) error {
		if name != nil {
			n, err := validName(*name, MaxSongName)
			if err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, `UPDATE local_songs SET name = ? WHERE id = ?`, n, id)
			if err != nil {
				return err
			}
			if c, _ := res.RowsAffected(); c == 0 {
				return ErrNotFound
			}
		}
		if artistSet {
			an, aid, err := resolveArtist(ctx, tx, artist)
			if err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, `UPDATE local_songs SET artist_name = ?, artist_id = ? WHERE id = ?`, an, aid, id)
			if err != nil {
				return err
			}
			if c, _ := res.RowsAffected(); c == 0 {
				return ErrNotFound
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return l.Song(ctx, id)
}

// Delete removes a local song and its overlay entries. The source file is never
// deleted (§7.8).
func (l *Library) Delete(ctx context.Context, id string) error {
	res, err := l.db.ExecContext(ctx, `DELETE FROM local_songs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

// Relink points a song whose file moved at the picked file (§7.7.1). The same
// SHA-256 relinks silently; a different file returns ErrDifferentFile with its facts,
// and the caller continues with Replace audio file.
func (l *Library) Relink(ctx context.Context, id, path string) (*Song, *Candidate, error) {
	s, err := l.Song(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	c := l.analyzeFile(path)
	if c.Problem != "" {
		return nil, &c, errors.New(c.Problem)
	}
	if c.SHA256 != s.SHA256 || c.ByteSize != s.ByteSize {
		return nil, &c, ErrDifferentFile
	}
	st, err := os.Stat(c.Path)
	if err != nil {
		return nil, nil, err
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE local_songs SET source_path = ?, observed_size = ?, observed_mtime = ?, health = 'ready' WHERE id = ?`,
		c.Path, st.Size(), st.ModTime().UnixNano(), id); err != nil {
		return nil, nil, err
	}
	s, err = l.Song(ctx, id)
	return s, nil, err
}

// ReplaceSource points a local song at another file and updates its facts after
// the suggestion step (§7.7, R-MUT-5). Overlay entries and position are kept;
// nothing is copied.
func (l *Library) ReplaceSource(ctx context.Context, id string, c Candidate, name string, artist *string) (*Song, error) {
	if l.InsideSongSleeve(c.Path) {
		return nil, ErrInSongSleeveFolder
	}
	if p := limitProblem(c.DurationMs, c.ByteSize); p != "" {
		return nil, errors.New(p)
	}
	n, err := validName(name, MaxSongName)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(c.Path)
	if err != nil {
		return nil, err
	}
	err = l.db.InTx(ctx, func(tx *sql.Tx) error {
		an, aid, err := resolveArtist(ctx, tx, artist)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE local_songs SET name = ?, artist_name = ?, artist_id = ?, duration_ms = ?, byte_size = ?, format = ?, sha256 = ?,
				source_path = ?, observed_size = ?, observed_mtime = ?, health = 'ready',
				sync_state = CASE WHEN sync_state = 'needs_attention' THEN 'local_only' ELSE sync_state END
			WHERE id = ?`, n, an, aid, c.DurationMs, c.ByteSize, c.Format, c.SHA256, c.Path, st.Size(), st.ModTime().UnixNano(), id)
		if err != nil {
			return err
		}
		if cnt, _ := res.RowsAffected(); cnt == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return l.Song(ctx, id)
}

// CheckHealth checks every local song's source file (§7.9 step 5, R-LOCAL-3). A
// quick check compares size and modification time; full (or a suspicious stat)
// also re-hashes. External changes only ever change health, never metadata.
func (l *Library) CheckHealth(ctx context.Context, full bool) (changed []string, err error) {
	type row struct {
		id, path, sha string
		size, mtime   int64
		health        string
	}
	rows, err := l.db.QueryContext(ctx, `SELECT id, source_path, sha256, observed_size, observed_mtime, health FROM local_songs`)
	if err != nil {
		return nil, err
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.path, &r.sha, &r.size, &r.mtime, &r.health); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	rows.Close()
	for _, r := range all {
		if ctx.Err() != nil {
			return changed, ctx.Err()
		}
		health := "ready"
		st, err := os.Stat(r.path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			health = "needs_relink"
		case errors.Is(err, fs.ErrPermission):
			health = "access_denied"
		case err != nil:
			health = "access_denied"
		default:
			suspicious := st.Size() != r.size || st.ModTime().UnixNano() != r.mtime
			if suspicious || full {
				f, err := os.Open(r.path)
				if err != nil {
					health = "access_denied"
					break
				}
				sum, n, err := probe.Hash(f)
				f.Close()
				switch {
				case err != nil:
					health = "access_denied"
				case sum != r.sha || n != r.size:
					health = "source_changed"
				default:
					// Same bytes (a touch or a copy back): remember the new stat.
					_, _ = l.db.ExecContext(ctx, `UPDATE local_songs SET observed_size = ?, observed_mtime = ? WHERE id = ?`, st.Size(), st.ModTime().UnixNano(), r.id)
				}
			}
		}
		if health != r.health {
			if _, err := l.db.ExecContext(ctx, `UPDATE local_songs SET health = ? WHERE id = ?`, health, r.id); err != nil {
				return changed, err
			}
			changed = append(changed, r.id)
		}
	}
	return changed, nil
}

// Overlay -------------------------------------------------------------------------

// playlistKeys returns every position key in a playlist: server items and this
// device's overlay entries share one key space (R-ORG-6).
func playlistKeys(ctx context.Context, tx *sql.Tx, db *accountdb.DB, playlistID string) ([]string, error) {
	var keys []string
	p, err := playlistDoc(ctx, tx, playlistID)
	if err != nil {
		return nil, err
	}
	if p != nil {
		for _, it := range p.Items {
			keys = append(keys, it.Position)
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT position FROM local_playlist_items WHERE playlist_id = ?`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, rows.Err()
}

func addOverlay(ctx context.Context, tx *sql.Tx, db *accountdb.DB, playlistID, songID string) error {
	p, err := playlistDoc(ctx, tx, playlistID)
	if err != nil {
		return err
	}
	if p == nil {
		return ErrNotFound
	}
	keys, err := playlistKeys(ctx, tx, db, playlistID)
	if err != nil {
		return err
	}
	last := ""
	if len(keys) > 0 {
		last = keys[len(keys)-1]
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO local_playlist_items (playlist_id, local_song_id, position) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
		playlistID, songID, position.After(last))
	return err
}

// AddToPlaylist places a local song at the end of an account playlist, on this
// device only (R-ORG-4).
func (l *Library) AddToPlaylist(ctx context.Context, songID, playlistID string) error {
	return l.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := l.songTx(ctx, tx, songID); err != nil {
			return err
		}
		return addOverlay(ctx, tx, l.db, playlistID, songID)
	})
}

func (l *Library) RemoveFromPlaylist(ctx context.Context, songID, playlistID string) error {
	_, err := l.db.ExecContext(ctx, `DELETE FROM local_playlist_items WHERE playlist_id = ? AND local_song_id = ?`, playlistID, songID)
	return err
}

// MoveInPlaylist moves an overlay entry between two neighbor keys ("" = an end).
func (l *Library) MoveInPlaylist(ctx context.Context, songID, playlistID, afterKey, beforeKey string) error {
	k, err := position.Between(afterKey, beforeKey)
	if err != nil {
		return err
	}
	res, err := l.db.ExecContext(ctx, `UPDATE local_playlist_items SET position = ? WHERE playlist_id = ? AND local_song_id = ?`, k, playlistID, songID)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

// MoveInLibrary moves a local song between two neighbor keys of the merged order.
// Moving a local item is local (R-ORG-6).
func (l *Library) MoveInLibrary(ctx context.Context, songID, afterKey, beforeKey string) error {
	k, err := position.Between(afterKey, beforeKey)
	if err != nil {
		return err
	}
	res, err := l.db.ExecContext(ctx, `UPDATE local_songs SET library_position = ? WHERE id = ?`, k, songID)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

func (l *Library) songTx(ctx context.Context, tx *sql.Tx, id string) (*Song, error) {
	s, err := scanSong(tx.QueryRowContext(ctx, `SELECT `+songColumns+` FROM local_songs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}
