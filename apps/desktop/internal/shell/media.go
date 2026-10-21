package shell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/songsdir"
)

// mediaServer serves song files to the webview's audio element on a loopback port
// (§9.4): local songs from their source, synced songs from the Songs folder, and
// songs not downloaded yet by redirecting to a short-lived download grant. Range
// requests make seeking work. A random secret in every URL keeps other local
// programs out.
type mediaServer struct {
	s      *Shell
	ln     net.Listener
	srv    *http.Server
	secret string
}

func startMedia(s *Shell) (*mediaServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	m := &mediaServer{s: s, ln: ln, secret: hex.EncodeToString(b)}
	m.srv = &http.Server{Handler: m, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = m.srv.Serve(ln) }()
	return m, nil
}

func (m *mediaServer) base() string { return "http://" + m.ln.Addr().String() + "/m/" + m.secret + "/" }

func (m *mediaServer) url(trackID string) string { return m.base() + url.PathEscape(trackID) }

func (m *mediaServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = m.srv.Shutdown(ctx)
}

var mimeTypes = map[string]string{"mp3": "audio/mpeg", "aac": "audio/mp4", "flac": "audio/flac", "wav": "audio/wav"}

func (m *mediaServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/m/"+m.secret+"/")
	if !ok || (r.Method != "GET" && r.Method != "HEAD") {
		http.NotFound(w, r)
		return
	}
	trackID, err := url.PathUnescape(rest)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a := m.s.current()
	if a == nil {
		http.Error(w, "signed out", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	var path, format string
	if isLocal(trackID) {
		song, err := a.lib.Song(ctx, songID(trackID))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		path, format = song.SourcePath, song.Format
	} else {
		t, err := a.db.Track(ctx, trackID)
		if err != nil || t == nil {
			http.NotFound(w, r)
			return
		}
		format = t.Format
		if path, err = a.dev.PlayablePath(ctx, trackID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if path == "" {
			// Not in the Songs folder yet: stream it (§9.1).
			if t.CurrentContentID == nil {
				http.NotFound(w, r)
				return
			}
			g, err := m.s.client.DownloadGrant(ctx, *t.CurrentContentID)
			if err != nil {
				if errors.Is(err, api.ErrConnection) {
					http.Error(w, "connection_required", http.StatusServiceUnavailable)
					return
				}
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			http.Redirect(w, r, g.URL, http.StatusFound)
			return
		}
	}
	f, err := os.Open(path)
	if err != nil {
		// Moved or unreadable source: the health check marks it (§7.6).
		go func() {
			if _, err := a.lib.CheckHealth(context.Background(), false); err == nil {
				m.s.emit("catalog", nil)
			}
		}()
		http.Error(w, "unavailable", http.StatusGone)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if ct, ok := mimeTypes[format]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, songsdir.Extension(format), st.ModTime(), f)
}
