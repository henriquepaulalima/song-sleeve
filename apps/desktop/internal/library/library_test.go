package library

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/accountdb"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
)

// writeWAV writes a silent 8 kHz mono 8-bit WAV of the given length.
func writeWAV(t *testing.T, path string, seconds float64, fill byte) {
	t.Helper()
	n := int(8000 * seconds)
	b := make([]byte, 44+n)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)    // PCM
	binary.LittleEndian.PutUint16(b[22:], 1)    // mono
	binary.LittleEndian.PutUint32(b[24:], 8000) // sample rate
	binary.LittleEndian.PutUint32(b[28:], 8000) // byte rate
	binary.LittleEndian.PutUint16(b[32:], 1)
	binary.LittleEndian.PutUint16(b[34:], 8)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n))
	for i := 44; i < len(b); i++ {
		b[i] = fill
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

type env struct {
	db     *accountdb.DB
	lib    *Library
	music  string
	sleeve string
	ctx    context.Context
}

func setup(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	db, err := accountdb.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	sleeve := filepath.Join(root, "Song Sleeve")
	_ = os.MkdirAll(filepath.Join(sleeve, "alex@example.com"), 0o755)
	return &env{db: db, lib: New(db, sleeve), music: filepath.Join(root, "Music"), sleeve: sleeve, ctx: context.Background()}
}

func ptr[T any](v T) *T { return &v }

func (e *env) snapshot(t *testing.T, cursor int64, tracks []api.Track, artists []api.Artist, playlists []api.Playlist) {
	t.Helper()
	if _, err := e.db.ApplySnapshot(e.ctx, []api.Snapshot{{Cursor: &cursor, Tracks: tracks, Artists: artists, Playlists: playlists}}); err != nil {
		t.Fatal(err)
	}
}

func (e *env) importFiles(t *testing.T, playlist string, paths ...string) []Song {
	t.Helper()
	cands, skipped, err := e.lib.Analyze(e.ctx, paths)
	if err != nil || len(skipped) > 0 {
		t.Fatalf("analyze: %v skipped %v", err, skipped)
	}
	items := make([]ImportItem, len(cands))
	for i, c := range cands {
		if c.Problem != "" {
			t.Fatalf("%s: %s", c.Path, c.Problem)
		}
		items[i] = ImportItem{Candidate: c, Name: c.Name}
	}
	songs, err := e.lib.Import(e.ctx, items, []string{playlist})
	if err != nil {
		t.Fatal(err)
	}
	return songs
}

func TestImportKeepsFilesWhereTheyAre(t *testing.T) {
	e := setup(t)
	src := filepath.Join(e.music, "Rehearsal take 3 - Wren Atlas.wav")
	writeWAV(t, src, 2, 0x80)
	before, _ := os.ReadFile(src)
	songs := e.importFiles(t, "", src)
	s := songs[0]
	if s.SourcePath != src || s.Format != "wav" || s.DurationMs != 2000 || s.SyncState != "local_only" || s.Health != "ready" {
		t.Fatalf("song: %+v", s)
	}
	if s.Name != "Rehearsal take 3 - Wren Atlas" {
		t.Fatalf("name from the filename: %q", s.Name)
	}
	after, _ := os.ReadFile(src)
	if string(before) != string(after) {
		t.Fatal("the source file changed")
	}
	entries, _ := os.ReadDir(filepath.Join(e.sleeve, "alex@example.com"))
	if len(entries) != 0 {
		t.Fatal("import copied something into the Song Sleeve folder")
	}
	// The import survives reopening the database (a restart).
	e.db.Close()
	db, err := accountdb.Open(filepath.Dir(e.db.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got, _ := New(db, e.sleeve).Songs(e.ctx); len(got) != 1 {
		t.Fatal("import lost after a restart")
	}
}

func TestAnalyzeFolderAndHints(t *testing.T) {
	e := setup(t)
	writeWAV(t, filepath.Join(e.music, "a.wav"), 1, 0x80)
	writeWAV(t, filepath.Join(e.music, "Band", "Copper Sky - The Low Tides.wav"), 1, 0x81)
	writeWAV(t, filepath.Join(e.music, ".hidden", "x.wav"), 1, 0x82)
	_ = os.WriteFile(filepath.Join(e.music, "cover.jpg"), []byte{0xFF, 0xD8, 0xFF}, 0o644)
	_ = os.WriteFile(filepath.Join(e.music, "notes.txt"), []byte("hello world, not audio"), 0o644)
	cands, skipped, err := e.lib.Analyze(e.ctx, []string{e.music})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 || len(skipped) != 2 {
		t.Fatalf("candidates %d skipped %v", len(cands), skipped)
	}
	for _, c := range cands {
		if filepath.Base(c.Path) == "Copper Sky - The Low Tides.wav" && c.ArtistHint != "The Low Tides" {
			t.Fatalf("artist hint: %q", c.ArtistHint)
		}
	}
}

func TestLimitsAndFolderRule(t *testing.T) {
	e := setup(t)
	long := filepath.Join(e.music, "concert.wav")
	writeWAV(t, long, 1800.5, 0x80)
	cands, _, _ := e.lib.Analyze(e.ctx, []string{long})
	if cands[0].Problem != "song_too_long" {
		t.Fatalf("31-minute file: %q", cands[0].Problem)
	}
	exact := filepath.Join(e.music, "exact.wav")
	writeWAV(t, exact, 1800, 0x80)
	if c, _, _ := e.lib.Analyze(e.ctx, []string{exact}); c[0].Problem != "" {
		t.Fatalf("exactly 30 minutes refused: %q", c[0].Problem)
	}
	// Files in the Song Sleeve folder only hold synced songs (R-LOCAL-4), even via a link.
	mirror := filepath.Join(e.sleeve, "alex@example.com", "x.wav")
	writeWAV(t, mirror, 1, 0x80)
	if c, _, _ := e.lib.Analyze(e.ctx, []string{mirror}); c[0].Problem != "in_song_sleeve_folder" {
		t.Fatalf("mirror accepted: %q", c[0].Problem)
	}
	link := filepath.Join(e.music, "link.wav")
	if err := os.Symlink(mirror, link); err == nil {
		if c, _, _ := e.lib.Analyze(e.ctx, []string{link}); c[0].Problem != "in_song_sleeve_folder" {
			t.Fatal("a symlink smuggled a mirror in")
		}
	}
	// Folder scans skip the Song Sleeve folder entirely.
	cands, _, _ = e.lib.Analyze(e.ctx, []string{filepath.Dir(e.sleeve)})
	for _, c := range cands {
		if e.lib.InsideSongSleeve(c.Path) {
			t.Fatal("folder scan entered the Song Sleeve folder")
		}
	}
	if _, err := e.lib.Import(e.ctx, []ImportItem{{Candidate: Candidate{Path: mirror, DurationMs: 1000, ByteSize: 10}, Name: "x"}}, nil); !errors.Is(err, ErrInSongSleeveFolder) {
		t.Fatalf("import of a mirror: %v", err)
	}
	if _, err := e.lib.Import(e.ctx, []ImportItem{{Candidate: Candidate{Path: exact, DurationMs: 1000, ByteSize: 10}, Name: "   "}}, nil); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("empty name: %v", err)
	}
}

func TestEditsArtistsAndReplicaFollowUps(t *testing.T) {
	e := setup(t)
	e.snapshot(t, 1, nil, []api.Artist{{ID: "art-1", Name: "The Low Tides", Revision: 1}}, nil)
	src := filepath.Join(e.music, "a.wav")
	writeWAV(t, src, 1, 0x80)
	s := e.importFiles(t, "", src)[0]
	// A typed name with the same normalized form links the server artist.
	s2, err := e.lib.Edit(e.ctx, s.ID, ptr("Morning Ferry"), true, ptr("  the LOW tides "))
	if err != nil {
		t.Fatal(err)
	}
	if s2.Name != "Morning Ferry" || s2.ArtistID == nil || *s2.ArtistID != "art-1" || *s2.ArtistName != "The Low Tides" {
		t.Fatalf("edit: %+v", s2)
	}
	// A renamed server artist renames linked local songs (F-10).
	if _, err := e.db.ApplyChanges(e.ctx, &api.Changes{Cursor: 2, Artists: []api.Artist{{ID: "art-1", Name: "The Low Tides (Band)", Revision: 2}}}); err != nil {
		t.Fatal(err)
	}
	s3, _ := e.lib.Song(e.ctx, s.ID)
	if *s3.ArtistName != "The Low Tides (Band)" {
		t.Fatalf("rename not followed: %q", *s3.ArtistName)
	}
	// A deleted server artist: the song keeps the name and loses the link.
	if _, err := e.db.ApplyChanges(e.ctx, &api.Changes{Cursor: 3, Deleted: map[string][]string{"artist": {"art-1"}}}); err != nil {
		t.Fatal(err)
	}
	s4, _ := e.lib.Song(e.ctx, s.ID)
	if s4.ArtistID != nil || *s4.ArtistName != "The Low Tides (Band)" {
		t.Fatalf("after artist delete: %+v", s4)
	}
	names, _ := e.lib.LocalOnlyArtistNames(e.ctx)
	if len(names) != 1 || names[0] != "The Low Tides (Band)" {
		t.Fatalf("local-only artist names: %v", names)
	}
	s5, _ := e.lib.Edit(e.ctx, s.ID, nil, true, nil)
	if s5.ArtistName != nil {
		t.Fatal("clearing the artist")
	}
}

func TestMergedOrderAndOverlay(t *testing.T) {
	e := setup(t)
	e.snapshot(t, 5,
		[]api.Track{{ID: "t1", Name: "Synced 1", LibraryPosition: "a0", State: "ready", Revision: 1}, {ID: "t2", Name: "Synced 2", LibraryPosition: "a1", State: "ready", Revision: 1}},
		nil,
		[]api.Playlist{{ID: "pl", Name: "Sunday Drive", Revision: 1, Items: []api.PlaylistItem{{TrackID: "t2", Position: "a0"}}}})
	a := filepath.Join(e.music, "a.wav")
	b := filepath.Join(e.music, "b.wav")
	writeWAV(t, a, 1, 0x80)
	writeWAV(t, b, 1, 0x81)
	songs := e.importFiles(t, "pl", a, b)
	lib, _ := e.lib.Library(e.ctx)
	var order []string
	for _, it := range lib {
		order = append(order, it.ID)
	}
	if len(order) != 4 || order[0] != "t1" || order[1] != "t2" || order[2] != songs[0].ID {
		t.Fatalf("new local songs go to the end: %v", order)
	}
	// Move a local song to the top of the merged order; it's a local change.
	if err := e.lib.MoveInLibrary(e.ctx, songs[1].ID, "", "a0"); err != nil {
		t.Fatal(err)
	}
	lib, _ = e.lib.Library(e.ctx)
	if lib[0].ID != songs[1].ID || lib[0].Kind != "local" {
		t.Fatalf("move to top: %v", lib[0])
	}
	items, _ := e.lib.Playlist(e.ctx, "pl")
	if len(items) != 3 || items[0].ID != "t2" || items[1].Kind != "local" {
		t.Fatalf("playlist with overlay: %+v", items)
	}
	if err := e.lib.MoveInPlaylist(e.ctx, songs[1].ID, "pl", "", items[0].Position); err != nil {
		t.Fatal(err)
	}
	items, _ = e.lib.Playlist(e.ctx, "pl")
	if items[0].ID != songs[1].ID {
		t.Fatal("overlay move")
	}
	entries, _ := e.lib.OverlayEntries(e.ctx, songs[0].ID)
	if len(entries) != 1 || entries[0] != "pl" {
		t.Fatalf("overlay entries: %v", entries)
	}
	if err := e.lib.AddToPlaylist(e.ctx, songs[0].ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("overlay into a missing playlist: %v", err)
	}
	// Another device deletes the playlist: this device drops its entries (§7.6).
	res, err := e.db.ApplyChanges(e.ctx, &api.Changes{Cursor: 6, Deleted: map[string][]string{"playlist": {"pl"}}})
	if err != nil || len(res.DeletedPlaylists) != 1 {
		t.Fatal(err)
	}
	if entries, _ := e.lib.OverlayEntries(e.ctx, songs[0].ID); len(entries) != 0 {
		t.Fatal("overlay kept for a deleted playlist")
	}
	// Deleting a local song removes it and its overlay; the file stays.
	if err := e.lib.Delete(e.ctx, songs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatal("deleting a local song deleted its file")
	}
}

func TestImportIntoSeveralPlaylists(t *testing.T) {
	e := setup(t)
	e.snapshot(t, 1, nil, nil, []api.Playlist{{ID: "p1", Name: "One", Revision: 1}, {ID: "p2", Name: "Two", Revision: 1}})
	a := filepath.Join(e.music, "a.wav")
	writeWAV(t, a, 1, 0x80)
	cands, _, err := e.lib.Analyze(e.ctx, []string{a})
	if err != nil {
		t.Fatal(err)
	}
	songs, err := e.lib.Import(e.ctx, []ImportItem{{Candidate: cands[0], Name: "a"}}, []string{"p1", "p2"})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := e.lib.OverlayEntries(e.ctx, songs[0].ID)
	if len(entries) != 2 {
		t.Fatalf("the song is in both playlists: %v", entries)
	}
	// One missing playlist fails the whole import; nothing is saved.
	b := filepath.Join(e.music, "b.wav")
	writeWAV(t, b, 1, 0x81)
	cands, _, _ = e.lib.Analyze(e.ctx, []string{b})
	if _, err := e.lib.Import(e.ctx, []ImportItem{{Candidate: cands[0], Name: "b"}}, []string{"p1", "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("import into a missing playlist: %v", err)
	}
	if lib, _ := e.lib.Library(e.ctx); len(lib) != 1 {
		t.Fatalf("a failed import saved songs: %d", len(lib))
	}
}

func TestSnapshotDropsOverlayForMissingPlaylists(t *testing.T) {
	e := setup(t)
	e.snapshot(t, 1, nil, nil, []api.Playlist{{ID: "pl", Name: "P", Revision: 1}})
	a := filepath.Join(e.music, "a.wav")
	writeWAV(t, a, 1, 0x80)
	s := e.importFiles(t, "pl", a)[0]
	e.snapshot(t, 9, nil, nil, nil)
	if entries, _ := e.lib.OverlayEntries(e.ctx, s.ID); len(entries) != 0 {
		t.Fatal("overlay survived a snapshot without its playlist")
	}
	if c, _ := e.db.Cursor(e.ctx); c != 9 {
		t.Fatalf("cursor %d", c)
	}
}

func TestReplicaRevisionsAndCursor(t *testing.T) {
	e := setup(t)
	e.snapshot(t, 3, []api.Track{{ID: "t", Name: "v3", LibraryPosition: "a0", State: "ready", Revision: 3}}, nil, nil)
	res, _ := e.db.ApplyChanges(e.ctx, &api.Changes{Cursor: 2, Tracks: []api.Track{{ID: "t", Name: "old", LibraryPosition: "a0", Revision: 2}}})
	if len(res.Tracks) != 0 {
		t.Fatal("an older revision was applied")
	}
	got, _ := e.db.Track(e.ctx, "t")
	if got.Name != "v3" {
		t.Fatalf("track %q", got.Name)
	}
	if c, _ := e.db.Cursor(e.ctx); c != 3 {
		t.Fatalf("cursor went backwards: %d", c)
	}
	res, _ = e.db.ApplyChanges(e.ctx, &api.Changes{
		Cursor: 7, Deleted: map[string][]string{"track": {"t"}},
		Events: []api.ChangeEvent{{EntityID: "x", EventType: "track_sync_failed", Payload: map[string]any{"reason": "stalled"}}},
	})
	if len(res.Tracks) != 1 || !res.Tracks[0].Deleted || len(res.SyncFailures) != 1 {
		t.Fatalf("delete result: %+v", res)
	}
	// Applying the same page again is harmless.
	res, _ = e.db.ApplyChanges(e.ctx, &api.Changes{Cursor: 7, Deleted: map[string][]string{"track": {"t"}}})
	if len(res.Tracks) != 0 {
		t.Fatal("re-applied delete reported a change")
	}
}

func TestHealthAndRelink(t *testing.T) {
	e := setup(t)
	a := filepath.Join(e.music, "a.wav")
	b := filepath.Join(e.music, "b.wav")
	c := filepath.Join(e.music, "c.wav")
	writeWAV(t, a, 1, 0x80)
	writeWAV(t, b, 1, 0x81)
	writeWAV(t, c, 1, 0x82)
	songs := e.importFiles(t, "", a, b, c)

	// Moved away: relink needed.
	moved := filepath.Join(e.music, "moved", "a.wav")
	_ = os.MkdirAll(filepath.Dir(moved), 0o755)
	_ = os.Rename(a, moved)
	// Edited in place (same size, new bytes and mtime): file changed.
	writeWAV(t, b, 1, 0x90)
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(b, future, future)
	// Touched only: same bytes, still ready.
	_ = os.Chtimes(c, future, future)

	changed, err := e.lib.CheckHealth(e.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 {
		t.Fatalf("changed: %v", changed)
	}
	health := func(id string) string { s, _ := e.lib.Song(e.ctx, id); return s.Health }
	if health(songs[0].ID) != "needs_relink" || health(songs[1].ID) != "source_changed" || health(songs[2].ID) != "ready" {
		t.Fatalf("health: %s %s %s", health(songs[0].ID), health(songs[1].ID), health(songs[2].ID))
	}
	// External changes never change saved metadata (R-LOCAL-3).
	if s, _ := e.lib.Song(e.ctx, songs[1].ID); s.SHA256 != songs[1].SHA256 {
		t.Fatal("metadata followed an external edit")
	}
	if os.Getuid() != 0 {
		_ = os.Chmod(c, 0)
		e.lib.CheckHealth(e.ctx, true)
		if health(songs[2].ID) != "access_denied" {
			t.Fatalf("unreadable file: %s", health(songs[2].ID))
		}
		_ = os.Chmod(c, 0o644)
	}

	// Relink: the same file relinks silently...
	s, _, err := e.lib.Relink(e.ctx, songs[0].ID, moved)
	if err != nil || s.Health != "ready" || s.SourcePath != moved {
		t.Fatalf("relink same file: %v %+v", err, s)
	}
	// ...a different file goes through Replace audio file.
	other := filepath.Join(e.music, "other.wav")
	writeWAV(t, other, 2, 0x70)
	_, cand, err := e.lib.Relink(e.ctx, songs[0].ID, other)
	if !errors.Is(err, ErrDifferentFile) || cand == nil {
		t.Fatalf("relink different file: %v", err)
	}
	r, err := e.lib.ReplaceSource(e.ctx, songs[0].ID, *cand, "New Name", ptr("Someone"))
	if err != nil || r.DurationMs != 2000 || r.SourcePath != other || r.Name != "New Name" || r.LibraryPosition != songs[0].LibraryPosition {
		t.Fatalf("replace source: %v %+v", err, r)
	}
}
