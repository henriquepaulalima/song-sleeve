package shell

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/library"
)

func sortPlaylistItems(items []api.PlaylistItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Position != items[j].Position {
			return items[i].Position < items[j].Position
		}
		return items[i].TrackID < items[j].TrackID
	})
}

var (
	reTrack         = regexp.MustCompile(`^/me/tracks/([^/]+)$`)
	rePlaylistSongs = regexp.MustCompile(`^/me/playlists/([^/]+)/songs$`)
	rePlaylistOrder = regexp.MustCompile(`^/me/playlists/([^/]+)/songs/order$`)
	reArtist        = regexp.MustCompile(`^/me/artists/(local-artist:[^/]*)$`)
	reReplacement   = regexp.MustCompile(`^/me/tracks/(local:[^/]+)/replacement$`)
)

// artistRef is the artist field of a song edit: an existing artist or a new name.
type artistRef struct {
	ID   *string `json:"id"`
	Name *string `json:"name"`
}

type orderRequest struct {
	ItemID           string  `json:"item_id"`
	AfterID          *string `json:"after_id"`
	BeforeID         *string `json:"before_id"`
	ExpectedRevision int64   `json:"expected_revision"`
}

// serveLocalAPI answers the server-API calls that concern local songs, the way the
// server would, so the shared screens work unchanged. It reports whether it
// handled the request; a call that also involves synced songs is forwarded for
// those.
func (s *Shell) serveLocalAPI(w http.ResponseWriter, r *http.Request, a *account, path string, body []byte) bool {
	ctx := r.Context()
	if m := reTrack.FindStringSubmatch(path); m != nil && isLocal(m[1]) {
		id := songID(m[1])
		switch r.Method {
		case "PATCH":
			s.editLocal(w, ctx, a, id, body)
		case "DELETE":
			// Local delete (§7.8): the link and its overlay entries go; the source
			// file is never touched (R-LOCAL-4).
			if err := a.lib.Delete(ctx, id); err != nil {
				writeLibErr(w, err)
				return true
			}
			s.emit("catalog", nil)
			writeJSON(w, 200, map[string]any{"deleted": true})
		default:
			return false
		}
		return true
	}
	if reReplacement.MatchString(path) {
		writeErr(w, 400, "invalid_request", "Use Update source for a song on this device.")
		return true
	}
	if reArtist.MatchString(path) {
		writeErr(w, 400, "invalid_request", "This artist is only used by songs on this device. Edit the songs to change it.")
		return true
	}
	if m := rePlaylistSongs.FindStringSubmatch(path); m != nil && (r.Method == "POST" || r.Method == "DELETE") {
		return s.playlistSongs(w, r, a, m[1], body)
	}
	if path == "/me/library/songs/order" && r.Method == "PUT" {
		return s.reorder(w, r, a, "", body)
	}
	if m := rePlaylistOrder.FindStringSubmatch(path); m != nil && r.Method == "PUT" {
		return s.reorder(w, r, a, m[1], body)
	}
	return false
}

func (s *Shell) editLocal(w http.ResponseWriter, ctx context.Context, a *account, id string, body []byte) {
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, 400, "invalid_request", "Invalid body.")
		return
	}
	var name *string
	if raw, ok := req["name"]; ok {
		if err := json.Unmarshal(raw, &name); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid name.")
			return
		}
	}
	artistSet := false
	var artist *string
	if raw, ok := req["artist"]; ok {
		artistSet = true
		var ref *artistRef
		if err := json.Unmarshal(raw, &ref); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid artist.")
			return
		}
		if ref != nil {
			n, err := a.artistName(ctx, ref)
			if err != nil {
				writeErr(w, 400, "invalid_field", "This artist no longer exists.")
				return
			}
			artist = &n
		}
	}
	song, err := a.lib.Edit(ctx, id, name, artistSet, artist)
	if err != nil {
		writeLibErr(w, err)
		return
	}
	s.emit("catalog", nil)
	writeJSON(w, 200, map[string]any{"track": songTrack(song)})
}

// artistName resolves an artist reference from the UI to the name a local song
// links by: an existing artist, a local-only artist, or a new name.
func (a *account) artistName(ctx context.Context, ref *artistRef) (string, error) {
	if ref.ID == nil {
		if ref.Name == nil {
			return "", errors.New("empty artist")
		}
		return *ref.Name, nil
	}
	id := *ref.ID
	if strings.HasPrefix(id, localArtistPrefix) {
		songs, err := a.lib.Songs(ctx)
		if err != nil {
			return "", err
		}
		for _, s := range songs {
			if s.ArtistName != nil && localArtistPrefix+library.NormalizeArtist(*s.ArtistName) == id {
				return *s.ArtistName, nil
			}
		}
		return "", library.ErrNotFound
	}
	artists, err := a.db.Artists(ctx)
	if err != nil {
		return "", err
	}
	for _, ar := range artists {
		if ar.ID == id {
			return ar.Name, nil
		}
	}
	return "", library.ErrNotFound
}

// playlistSongs adds or removes songs: local ones in the overlay (no connection
// needed, R-ORG-4), synced ones on the server.
func (s *Shell) playlistSongs(w http.ResponseWriter, r *http.Request, a *account, playlistID string, body []byte) bool {
	var req map[string]json.RawMessage
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	var ids []string
	if json.Unmarshal(req["track_ids"], &ids) != nil {
		return false
	}
	var local, synced []string
	for _, id := range ids {
		if isLocal(id) {
			local = append(local, songID(id))
		} else {
			synced = append(synced, id)
		}
	}
	if len(local) == 0 {
		return false
	}
	ctx := r.Context()
	var serverDoc *api.Playlist
	if len(synced) > 0 {
		req["track_ids"], _ = json.Marshal(synced)
		fwd, _ := json.Marshal(req)
		status, _, resp, err := s.forward(ctx, r.Method, "/me/playlists/"+playlistID+"/songs", "", r.Header, fwd)
		if err != nil {
			s.writeForwardErr(w, r, "", err)
			return true
		}
		if status >= 300 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(resp)
			return true
		}
		var out struct {
			Playlist api.Playlist `json:"playlist"`
		}
		if json.Unmarshal(resp, &out) == nil {
			serverDoc = &out.Playlist
		}
		a.reconcileSoon()
	}
	if p, err := a.db.Playlist(ctx, playlistID); err == nil && p == nil {
		// Created moments ago: catch the replica up before adding overlay entries.
		_ = a.dev.Reconcile(ctx)
	}
	for _, id := range local {
		var err error
		if r.Method == "POST" {
			err = a.lib.AddToPlaylist(ctx, id, playlistID)
		} else {
			err = a.lib.RemoveFromPlaylist(ctx, id, playlistID)
		}
		if err != nil {
			writeLibErr(w, err)
			return true
		}
	}
	s.emit("catalog", nil)
	if serverDoc == nil {
		p, err := a.db.Playlist(ctx, playlistID)
		if err != nil || p == nil {
			writeErr(w, 404, "not_found", "This playlist no longer exists.")
			return true
		}
		serverDoc = p
	}
	raw, _ := json.Marshal(map[string]any{"playlist": serverDoc})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(a.mergeOverlay(ctx, raw))
	return true
}

// reorder moves an item in the merged library order (playlistID == "") or in a
// merged playlist. Local songs move locally; a synced song moves on the server,
// placed between its nearest synced neighbors (R-ORG-6).
func (s *Shell) reorder(w http.ResponseWriter, r *http.Request, a *account, playlistID string, body []byte) bool {
	var req orderRequest
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	neighborLocal := (req.AfterID != nil && isLocal(*req.AfterID)) || (req.BeforeID != nil && isLocal(*req.BeforeID))
	if !isLocal(req.ItemID) && !neighborLocal {
		return false
	}
	ctx := r.Context()
	var items []library.Item
	var err error
	if playlistID == "" {
		items, err = a.lib.Library(ctx)
	} else {
		items, err = a.lib.Playlist(ctx, playlistID)
	}
	if err != nil {
		writeLibErr(w, err)
		return true
	}
	ids := make([]string, len(items))
	pos := map[string]string{}
	for i, it := range items {
		id := it.ID
		if it.Kind == "local" {
			id = localID(id)
		}
		ids[i] = id
		pos[id] = it.Position
	}
	if isLocal(req.ItemID) {
		key := func(p *string) string {
			if p == nil {
				return ""
			}
			return pos[*p]
		}
		if playlistID == "" {
			err = a.lib.MoveInLibrary(ctx, songID(req.ItemID), key(req.AfterID), key(req.BeforeID))
		} else {
			err = a.lib.MoveInPlaylist(ctx, songID(req.ItemID), playlistID, key(req.AfterID), key(req.BeforeID))
		}
		if err != nil {
			writeLibErr(w, err)
			return true
		}
		s.emit("catalog", nil)
		if playlistID == "" {
			song, err := a.lib.Song(ctx, songID(req.ItemID))
			if err != nil {
				writeLibErr(w, err)
				return true
			}
			writeJSON(w, 200, map[string]any{"track": songTrack(song)})
			return true
		}
		p, err := a.db.Playlist(ctx, playlistID)
		if err != nil || p == nil {
			writeErr(w, 404, "not_found", "This playlist no longer exists.")
			return true
		}
		merged, _ := a.mergedPlaylist(ctx, *p)
		writeJSON(w, 200, map[string]any{"playlist": merged})
		return true
	}
	// A synced song next to local ones: use the nearest synced neighbors.
	idx := func(id string) int {
		for i, x := range ids {
			if x == id {
				return i
			}
		}
		return -1
	}
	nearest := func(from *string, step int) *string {
		if from == nil {
			return nil
		}
		for i := idx(*from); i >= 0 && i < len(ids); i += step {
			if !isLocal(ids[i]) && ids[i] != req.ItemID {
				v := ids[i]
				return &v
			}
		}
		return nil
	}
	req.AfterID = nearest(req.AfterID, -1)
	req.BeforeID = nearest(req.BeforeID, 1)
	fwd, _ := json.Marshal(req)
	path := "/me/library/songs/order"
	if playlistID != "" {
		path = "/me/playlists/" + playlistID + "/songs/order"
	}
	status, _, resp, err := s.forward(ctx, "PUT", path, "", r.Header, fwd)
	if err != nil {
		s.writeForwardErr(w, r, "", err)
		return true
	}
	if status < 300 {
		a.reconcileSoon()
		resp = a.mergeOverlay(ctx, resp)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
	return true
}

// The shell's own API ----------------------------------------------------------

type stateDoc struct {
	SignedIn bool              `json:"signed_in"`
	Me       *api.Me           `json:"me,omitempty"`
	Online   bool              `json:"online"`
	Version  string            `json:"version"`
	MediaURL string            `json:"media_url"`
	Keyring  bool              `json:"keyring"`
	Folder   map[string]string `json:"folder,omitempty"`
	DataDir  string            `json:"data_dir"`
	OS       string            `json:"os"`
}

func decode(r *http.Request, dst any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20)).Decode(dst)
}

func (s *Shell) serveLocal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/local")
	if path == "/state" {
		doc := stateDoc{Online: s.online.Load(), Version: s.cfg.Version, MediaURL: s.media.base(), Keyring: s.creds.Keyring, DataDir: s.cfg.DataDir, OS: runtime.GOOS}
		if a := s.current(); a != nil {
			doc.SignedIn = true
			doc.Me = a.Me()
			doc.Folder = map[string]string{"root": a.songs.Root, "account": a.songs.AccountDir}
		}
		writeJSON(w, 200, doc)
		return
	}
	if strings.HasPrefix(path, "/update") || path == "/open-url" {
		s.serveUpdate(w, r, path)
		return
	}
	if path == "/tray" && r.Method == "PUT" {
		// Signed out too: the tray still shows and quits.
		var st TrayState
		if err := decode(r, &st); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid body.")
			return
		}
		if s.cfg.Hooks.Tray != nil {
			s.cfg.Hooks.Tray(st)
		}
		w.WriteHeader(204)
		return
	}
	a := s.current()
	if a == nil {
		writeErr(w, 401, "unauthenticated", "Sign in to continue.")
		return
	}
	switch {
	case path == "/catalog" && r.Method == "GET":
		doc, err := a.catalog(ctx)
		if err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
		writeJSON(w, 200, doc)

	case path == "/player" && r.Method == "GET":
		// The saved player state, restored paused after a restart (R-PLAY-5…8).
		b, err := os.ReadFile(filepath.Join(a.dir, playerFile))
		if err != nil || !json.Valid(b) {
			b = []byte("null")
		}
		writeJSON(w, 200, map[string]json.RawMessage{"snapshot": b})

	case path == "/player" && r.Method == "PUT":
		var req struct {
			Snapshot json.RawMessage `json:"snapshot"`
		}
		if err := decode(r, &req); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid body.")
			return
		}
		if err := savePlayer(a.dir, req.Snapshot); err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
		w.WriteHeader(204)

	case path == "/pick" && r.Method == "POST":
		var req struct {
			Folder bool   `json:"folder"`
			Title  string `json:"title"`
		}
		_ = decode(r, &req)
		if s.cfg.Hooks.PickFiles == nil {
			writeErr(w, 400, "invalid_request", "No file picker.")
			return
		}
		paths, err := s.cfg.Hooks.PickFiles(req.Folder, req.Title)
		if err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"paths": paths})

	case path == "/analyze" && r.Method == "POST":
		var req struct {
			Paths []string `json:"paths"`
		}
		if err := decode(r, &req); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid body.")
			return
		}
		cands, skipped, err := a.lib.Analyze(ctx, req.Paths)
		if err != nil {
			writeLibErr(w, err)
			return
		}
		if cands == nil {
			cands = []library.Candidate{}
		}
		writeJSON(w, 200, map[string]any{"candidates": cands, "skipped": len(skipped)})

	case path == "/import" && r.Method == "POST":
		var req struct {
			Items       []library.ImportItem `json:"items"`
			PlaylistIDs []string             `json:"playlist_ids"`
			PlaylistID  string               `json:"playlist_id"` // older callers: one playlist
			Sync        bool                 `json:"sync"`
		}
		if err := decode(r, &req); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid body.")
			return
		}
		if req.PlaylistID != "" {
			req.PlaylistIDs = append(req.PlaylistIDs, req.PlaylistID)
		}
		songs, err := a.lib.Import(ctx, req.Items, req.PlaylistIDs)
		if err != nil {
			writeLibErr(w, err)
			return
		}
		if req.Sync && a.Me().CloudSyncEnabled {
			ids := make([]string, len(songs))
			for i, sg := range songs {
				ids[i] = sg.ID
			}
			if _, err := a.dev.QueueSync(ctx, ids); err != nil {
				writeLibErr(w, err)
				return
			}
		}
		s.emit("catalog", nil)
		writeJSON(w, 200, map[string]any{"added": len(songs)})

	case path == "/sync" && r.Method == "POST":
		// Sync to cloud for local songs (§7.2); retry covers failed ones.
		var req struct {
			IDs    []string `json:"ids"`
			Retry  bool     `json:"retry"`
			Cancel bool     `json:"cancel"`
		}
		if err := decode(r, &req); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid body.")
			return
		}
		if !a.Me().CloudSyncEnabled {
			writeErr(w, 403, "cloud_sync_unavailable", "Cloud sync isn't available for this account yet.")
			return
		}
		ids := make([]string, len(req.IDs))
		for i, id := range req.IDs {
			ids[i] = songID(id)
		}
		var n int
		var err error
		switch {
		case req.Cancel:
			for _, id := range ids {
				if err = a.dev.CancelSync(ctx, id); err != nil {
					break
				}
				n++
			}
		case req.Retry:
			n, err = a.dev.RetryFailed(ctx, ids)
		default:
			n, err = a.dev.QueueSync(ctx, ids)
		}
		if err != nil {
			writeLibErr(w, err)
			return
		}
		s.emit("catalog", nil)
		writeJSON(w, 200, map[string]any{"queued": n})

	case path == "/relink" && r.Method == "POST":
		// Relink (§7.6): pick the moved file; the same file relinks silently.
		var req struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		}
		if err := decode(r, &req); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid body.")
			return
		}
		song, cand, err := a.lib.Relink(ctx, songID(req.ID), req.Path)
		if errors.Is(err, library.ErrDifferentFile) {
			writeJSON(w, 409, map[string]any{"error": map[string]any{"code": "different_file", "message": "This is a different file."}, "candidate": cand})
			return
		}
		if err != nil {
			writeLibErr(w, err)
			return
		}
		s.emit("catalog", nil)
		writeJSON(w, 200, map[string]any{"track": songTrack(song)})

	case path == "/replace-source" && r.Method == "POST":
		var req struct {
			ID        string            `json:"id"`
			Candidate library.Candidate `json:"candidate"`
			Name      string            `json:"name"`
			Artist    *string           `json:"artist"`
		}
		if err := decode(r, &req); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid body.")
			return
		}
		song, err := a.lib.ReplaceSource(ctx, songID(req.ID), req.Candidate, req.Name, req.Artist)
		if err != nil {
			writeLibErr(w, err)
			return
		}
		s.emit("catalog", nil)
		writeJSON(w, 200, map[string]any{"track": songTrack(song)})

	case path == "/health" && r.Method == "POST":
		changed, err := a.lib.CheckHealth(ctx, true)
		if err != nil {
			writeLibErr(w, err)
			return
		}
		if len(changed) > 0 {
			s.emit("catalog", nil)
		}
		writeJSON(w, 200, map[string]any{"changed": len(changed)})

	case path == "/check-folder" && r.Method == "POST":
		rep, err := a.dev.CheckSongsFolder(ctx)
		if err != nil {
			if errors.Is(err, api.ErrConnection) {
				writeErr(w, 503, "connection_required", "Connect to the server to check the Songs folder.")
				return
			}
			writeErr(w, 500, "internal", err.Error())
			return
		}
		// The Settings summary (§8.3): restored, extra files removed, still to download.
		out := map[string]any{"caught_up": rep.CaughtUp, "restored": 0, "removed": rep.Removed, "need_download": 0, "deferred": 0}
		if c := rep.Check; c != nil {
			out["restored"] = c.Renamed + c.Restored
			out["removed"] = rep.Removed + c.Removed
			out["need_download"] = max(0, len(c.NeedsDownload)-rep.Downloaded)
			out["deferred"] = c.DeferredExtras
		}
		writeJSON(w, 200, out)

	case path == "/reveal" && r.Method == "POST":
		var req struct {
			ID     string `json:"id"`
			Folder bool   `json:"folder"`
		}
		_ = decode(r, &req)
		target := a.songs.AccountDir
		if req.ID != "" {
			song, err := a.lib.Song(ctx, songID(req.ID))
			if err != nil {
				writeLibErr(w, err)
				return
			}
			target = filepath.Dir(song.SourcePath)
		}
		if s.cfg.Hooks.Open == nil {
			writeErr(w, 400, "invalid_request", "Can't open folders here.")
			return
		}
		if _, err := os.Stat(target); err != nil {
			writeErr(w, 404, "not_found", "This folder no longer exists.")
			return
		}
		if err := s.cfg.Hooks.Open(target); err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
		w.WriteHeader(204)

	case path == "/play" && r.Method == "POST":
		var req struct {
			TrackID *string `json:"track_id"`
		}
		_ = decode(r, &req)
		writeJSON(w, 200, s.play(ctx, a, req.TrackID))

	case path == "/logout" && r.Method == "POST":
		var req struct {
			Remove bool `json:"remove"`
		}
		_ = decode(r, &req)
		if err := s.SignOut(ctx, req.Remove); err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
		w.WriteHeader(204)

	default:
		http.NotFound(w, r)
	}
}

// play pins the track that starts playing and releases the previous pin at the
// playback boundary (R-PIN-1…3), then returns the URL the engine loads.
func (s *Shell) play(ctx context.Context, a *account, trackID *string) map[string]any {
	a.playingMu.Lock()
	prev := a.playing
	a.playing = ""
	if trackID != nil {
		a.playing = *trackID
	}
	a.playingMu.Unlock()
	if prev != "" && !isLocal(prev) && (trackID == nil || *trackID != prev) {
		go func() { _ = a.dev.Unpin(context.Background(), prev) }()
	}
	if trackID == nil {
		return map[string]any{}
	}
	if !isLocal(*trackID) {
		a.dev.Pin(*trackID)
	}
	return map[string]any{"url": s.media.url(*trackID)}
}

const playerFile = "player.json"

// savePlayer writes the player snapshot atomically; null removes it.
func savePlayer(dir string, snap json.RawMessage) error {
	p := filepath.Join(dir, playerFile)
	if len(snap) == 0 || string(snap) == "null" {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if len(snap) > 1<<20 {
		return errors.New("player state too large")
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, snap, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
