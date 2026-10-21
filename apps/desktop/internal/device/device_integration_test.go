//go:build integration

package device

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/accountdb"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/library"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/songsdir"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/testserver"
)

var (
	baseURL  string
	syncUser = fmt.Sprintf("desktop-%s@example.com", uuid.NewString()[:8])
)

func TestMain(m *testing.M) { testserver.Main(m, []string{syncUser}, &baseURL) }

type install struct {
	t     *testing.T
	dev   *Device
	db    *accountdb.DB
	lib   *library.Library
	dir   *songsdir.Dir
	api   *api.Client
	me    *api.Me
	music string
	ctx   context.Context
}

// newInstall signs an account in on a fresh installation (its own data dir and
// Song Sleeve folder), registering it if needed.
func newInstall(t *testing.T, email string) *install {
	t.Helper()
	root := t.TempDir()
	c := api.New(baseURL, "desktop", "0.1.0")
	ctx := context.Background()
	var auth api.AuthResponse
	dev := map[string]any{"installation_id": uuid.NewString(), "label": "Test PC"}
	err := c.Do(ctx, "POST", "/auth/register", map[string]any{"email": email, "password": "saltwater-hymn", "accepted_terms": true, "terms_version": "2026-10-draft", "device": dev}, &auth)
	if api.Code(err) == "email_unavailable" {
		err = c.Do(ctx, "POST", "/auth/login", map[string]any{"email": email, "password": "saltwater-hymn", "device": dev}, &auth)
	}
	if err != nil {
		t.Fatal(err)
	}
	c.SetToken(auth.Token)
	db, err := accountdb.Open(filepath.Join(root, "data", auth.Account.ID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	sleeve := filepath.Join(root, "Song Sleeve")
	dir, err := songsdir.Open(sleeve, songsdir.AccountFolderName(email), auth.Account.ID, filepath.Join(root, "data", auth.Account.ID, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	lib := library.New(db, sleeve)
	in := &install{t: t, db: db, lib: lib, dir: dir, api: c, me: &auth.Account, music: filepath.Join(root, "Music"), ctx: ctx}
	in.dev = New(Config{DB: db, Library: lib, Dir: dir, API: c, Me: func() *api.Me { return in.me }, TempDir: filepath.Join(root, "data", "tmp")})
	in.dev.Start(ctx)
	t.Cleanup(in.dev.Stop)
	in.reconcile()
	return in
}

func (in *install) reconcile() *ReconcileReport {
	in.t.Helper()
	r, err := in.dev.ReconcileReport(in.ctx, songsdir.Quick)
	if err != nil {
		in.t.Fatal(err)
	}
	return r
}

func wav(t *testing.T, path string, seconds float64, fill byte) {
	t.Helper()
	n := int(8000 * seconds)
	b := make([]byte, 44+n)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 8000)
	binary.LittleEndian.PutUint32(b[28:], 8000)
	binary.LittleEndian.PutUint16(b[32:], 1)
	binary.LittleEndian.PutUint16(b[34:], 8)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n))
	for i := 44; i < len(b); i++ {
		b[i] = fill + byte(i%7)
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (in *install) importWAV(name string, seconds float64, fill byte, playlist string) library.Song {
	in.t.Helper()
	path := filepath.Join(in.music, name)
	wav(in.t, path, seconds, fill)
	cands, _, err := in.lib.Analyze(in.ctx, []string{path})
	if err != nil || len(cands) != 1 || cands[0].Problem != "" {
		in.t.Fatalf("analyze: %v %+v", err, cands)
	}
	songs, err := in.lib.Import(in.ctx, []library.ImportItem{{Candidate: cands[0], Name: strings.TrimSuffix(name, ".wav")}}, []string{playlist})
	if err != nil {
		in.t.Fatal(err)
	}
	return songs[0]
}

func (in *install) waitFor(what string, fn func() bool) {
	in.t.Helper()
	end := time.Now().Add(40 * time.Second)
	for time.Now().Before(end) {
		in.reconcile()
		if fn() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	in.t.Fatalf("timed out waiting for %s", what)
}

func fileSHA(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestLocalSongSyncsAndMirrorsToAnotherDevice(t *testing.T) {
	a := newInstall(t, syncUser)
	// A server playlist, then a local song placed in it through the overlay.
	var pl struct {
		Playlist api.Playlist `json:"playlist"`
	}
	if err := a.api.Mutate(a.ctx, "POST", "/me/playlists", "", map[string]any{"name": "Road Trip"}, &pl); err != nil {
		t.Fatal(err)
	}
	a.reconcile()
	song := a.importWAV("Lowland - Wren Atlas.wav", 3, 0x40, pl.Playlist.ID)
	if _, err := a.lib.Edit(a.ctx, song.ID, nil, true, ptr("Wren Atlas")); err != nil {
		t.Fatal(err)
	}
	srcBefore := fileSHA(song.SourcePath)

	if n, err := a.dev.QueueSync(a.ctx, []string{song.ID}); err != nil || n != 1 {
		t.Fatalf("queue: %d %v", n, err)
	}
	var trackID string
	a.waitFor("sync to finish", func() bool {
		if _, err := a.lib.Song(a.ctx, song.ID); err == library.ErrNotFound {
			tracks, _ := a.db.Tracks(a.ctx)
			for _, tr := range tracks {
				if tr.Name == "Lowland - Wren Atlas" && tr.State == "ready" {
					trackID = tr.ID
					return true
				}
			}
		}
		return false
	})
	// The device plays the Songs mirror now; the source file is untouched (R-CLOUD-4).
	path, err := a.dev.PlayablePath(a.ctx, trackID)
	if err != nil || path == "" || fileSHA(path) != srcBefore {
		t.Fatalf("mirror: %q %v", path, err)
	}
	if fileSHA(song.SourcePath) != srcBefore {
		t.Fatal("syncing changed the source file")
	}
	if filepath.Dir(path) != a.dir.AccountDir || !strings.HasSuffix(path, ".wav") || strings.Contains(path, ".syncing.") {
		t.Fatalf("mirror name: %s", path)
	}
	// The overlay entry became a server membership (R-ORG-4) and the artist was created.
	p, _ := a.db.Playlist(a.ctx, pl.Playlist.ID)
	if len(p.Items) != 1 || p.Items[0].TrackID != trackID {
		t.Fatalf("playlist membership: %+v", p.Items)
	}
	tr, _ := a.db.Track(a.ctx, trackID)
	if tr.ArtistID == nil {
		t.Fatal("the local artist name did not become a server artist")
	}

	// A second installation of the same account downloads it into its own folder.
	b := newInstall(t, syncUser)
	bpath, _ := b.dev.PlayablePath(b.ctx, trackID)
	if bpath == "" || fileSHA(bpath) != srcBefore {
		t.Fatalf("second device mirror: %q", bpath)
	}

	// Folder check on open: deleted, edited and extra files are repaired (§7.13).
	os.Remove(bpath)
	os.WriteFile(filepath.Join(b.dir.AccountDir, "added-by-hand.mp3"), []byte("x"), 0o644)
	r := b.reconcile()
	if r.Downloaded != 1 || r.Check.Removed != 1 || fileSHA(bpath) != srcBefore {
		t.Fatalf("repair: %+v %+v", r, r.Check)
	}
	os.WriteFile(bpath, []byte("edited by hand"), 0o644)
	r, _ = b.dev.CheckSongsFolder(b.ctx)
	if fileSHA(bpath) != srcBefore || r.Check.Restored != 1 {
		t.Fatalf("restore after edit: %+v", r.Check)
	}

	// Pins: B is playing the song when its audio is replaced elsewhere (R-PIN-1…3).
	b.dev.Pin(trackID)
	replacement := filepath.Join(a.music, "lowland-v2.wav")
	wav(t, replacement, 4, 0x60)
	data, _ := os.ReadFile(replacement)
	sum := sha256.Sum256(data)
	var rep struct {
		Operation api.OperationDoc `json:"operation"`
	}
	if err := a.api.Mutate(a.ctx, "POST", "/me/tracks/"+trackID+"/replacement", "", map[string]any{
		"expected_revision": tr.Revision, "name": tr.Name, "artist": nil,
		"duration_ms": 4000, "byte_size": len(data), "format": "wav", "sha256": hex.EncodeToString(sum[:]),
	}, &rep); err != nil {
		t.Fatal(err)
	}
	uploadDirect(t, a, rep.Operation, data)
	b.waitFor("staged replacement", func() bool {
		var readiness string
		_ = b.db.QueryRowContext(b.ctx, `SELECT readiness FROM synced_files WHERE track_id = ?`, trackID).Scan(&readiness)
		return readiness == "pending_apply"
	})
	if fileSHA(bpath) != srcBefore {
		t.Fatal("the playing file changed under the pin")
	}
	if err := b.dev.Unpin(b.ctx, trackID); err != nil {
		t.Fatal(err)
	}
	newPath, _ := b.dev.PlayablePath(b.ctx, trackID)
	if fileSHA(newPath) != hex.EncodeToString(sum[:]) {
		t.Fatal("the replacement was not installed when the pin was released")
	}
	if _, err := os.Stat(bpath); err == nil {
		t.Fatal("the retired file was kept after the pin's release")
	}

	// Deleting the song elsewhere removes the mirror here too.
	if err := a.api.Mutate(a.ctx, "DELETE", "/me/tracks/"+trackID, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	b.waitFor("mirror removal", func() bool { _, err := os.Stat(newPath); return os.IsNotExist(err) })
	// The source file of the original local song is never touched (R-LOCAL-4).
	if fileSHA(song.SourcePath) != srcBefore {
		t.Fatal("deleting the synced song touched the original file")
	}
}

// uploadDirect sends a replacement's bytes the way another device would.
func uploadDirect(t *testing.T, in *install, op api.OperationDoc, data []byte) {
	t.Helper()
	var g struct {
		Parts []struct {
			URL string `json:"url"`
		} `json:"parts"`
	}
	if err := in.api.Do(in.ctx, "POST", "/me/operations/"+op.ID+"/upload-grant", map[string]any{"part_numbers": []int{1}}, &g); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("PUT", g.Parts[0].URL, bytes.NewReader(data))
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("put: %v", err)
	}
	etag := res.Header.Get("ETag")
	res.Body.Close()
	if err := in.api.Do(in.ctx, "POST", "/me/operations/"+op.ID+"/uploaded", map[string]any{"parts": []map[string]any{{"part_number": 1, "etag": etag}}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPreCheckBlocksChangedSources(t *testing.T) {
	a := newInstall(t, syncUser)
	song := a.importWAV("changed.wav", 2, 0x10, "")
	// The file changes after import: the sync stops before any track document exists (F-21).
	wav(t, song.SourcePath, 2, 0x55)
	if _, err := a.dev.QueueSync(a.ctx, []string{song.ID}); err != nil {
		t.Fatal(err)
	}
	a.waitFor("needs attention", func() bool {
		s, _ := a.lib.Song(a.ctx, song.ID)
		return s.SyncState == "needs_attention"
	})
	tracks, _ := a.db.Tracks(a.ctx)
	for _, tr := range tracks {
		if tr.Name == "changed" {
			t.Fatal("a track document was created for a changed source")
		}
	}
	// Try again re-queues it (it will fail the same way until replaced).
	if n, _ := a.dev.RetryFailed(a.ctx, []string{song.ID}); n != 1 {
		t.Fatal("retry did not re-queue a needs-attention song")
	}
}

func TestQueueSurvivesRestartAndCancel(t *testing.T) {
	a := newInstall(t, syncUser)
	a.dev.Stop() // no workers: songs stay queued
	s1 := a.importWAV("q1.wav", 1, 0x20, "")
	s2 := a.importWAV("q2.wav", 1, 0x21, "")
	if n, _ := a.dev.QueueSync(a.ctx, []string{s1.ID, s2.ID}); n != 2 {
		t.Fatal("queue")
	}
	if err := a.dev.CancelSync(a.ctx, s2.ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.lib.Song(a.ctx, s2.ID); s.SyncState != "local_only" {
		t.Fatalf("cancelled queued song: %s", s.SyncState)
	}
	// "Restart": a new service over the same database picks up the queue (R-CLOUD-9).
	dev2 := New(Config{DB: a.db, Library: a.lib, Dir: a.dir, API: a.api, Me: func() *api.Me { return a.me }, TempDir: t.TempDir()})
	dev2.Start(a.ctx)
	defer dev2.Stop()
	a.dev = dev2
	a.waitFor("queued song synced after restart", func() bool {
		_, err := a.lib.Song(a.ctx, s1.ID)
		return err == library.ErrNotFound
	})
	if s, _ := a.lib.Song(a.ctx, s2.ID); s.SyncState != "local_only" {
		t.Fatal("a cancelled song synced anyway")
	}
}

func TestAccountWithoutCloudSync(t *testing.T) {
	a := newInstall(t, fmt.Sprintf("nosync-%s@example.com", uuid.NewString()[:8]))
	song := a.importWAV("local.wav", 1, 0x30, "")
	if _, err := a.dev.QueueSync(a.ctx, []string{song.ID}); api.Code(err) != "cloud_sync_unavailable" {
		t.Fatalf("sync without cloud sync: %v", err)
	}
	// Local songs still import, edit and play from their source.
	if s, _ := a.lib.Song(a.ctx, song.ID); s.SyncState != "local_only" {
		t.Fatal("song state changed")
	}
}

func TestAutoSyncScope(t *testing.T) {
	a := newInstall(t, syncUser)
	if err := a.api.Mutate(a.ctx, "PUT", "/me/sync-scopes/library", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	song := a.importWAV("auto.wav", 1, 0x70, "")
	a.waitFor("auto-sync of a song in an enabled scope", func() bool {
		_, err := a.lib.Song(a.ctx, song.ID)
		return err == library.ErrNotFound
	})
	_ = a.api.Mutate(a.ctx, "POST", "/me/sync-scopes/library/pause", "", nil, nil)
}

func ptr[T any](v T) *T { return &v }
