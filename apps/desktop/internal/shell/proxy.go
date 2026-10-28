package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/library"
)

// HeaderOffline marks a proxy answer for a request that never reached the server,
// so the UI reports connection_required (R-MUT-1). HeaderUnknown marks one that was
// sent without an answer: the UI keeps its request ID (R-MUT-4).
const (
	HeaderOffline = "X-Song-Sleeve-Offline"
	HeaderUnknown = "X-Song-Sleeve-Unknown"
)

// ServeHTTP is the webview's backend: /api goes to the server (or the local
// library), /local is the shell's own API.
func (s *Shell) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/"):
		s.serveAPI(w, r)
	case strings.HasPrefix(r.URL.Path, "/local/"):
		s.serveLocal(w, r)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg}})
}

func writeLibErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeErr(w, 404, "not_found", "This song is no longer on this device.")
	case errors.Is(err, library.ErrInvalidName):
		writeErr(w, 400, "invalid_field", "Enter a valid name.")
	case errors.Is(err, library.ErrLimitReached):
		writeErr(w, 400, "limit_reached", "This device has reached its song limit.")
	case errors.Is(err, library.ErrInSongSleeveFolder):
		writeErr(w, 400, "invalid_request", "This file is in the Song Sleeve folder, which only holds synced songs.")
	default:
		writeErr(w, 500, "internal", err.Error())
	}
}

func (s *Shell) serveAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api")
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeErr(w, 400, "invalid_request", "Could not read the request.")
		return
	}
	if a := s.current(); a != nil {
		if s.serveLocalAPI(w, r, a, path, body) {
			return
		}
	}
	isAuth := r.Method == "POST" && (path == "/auth/login" || path == "/auth/register")
	if isAuth {
		body = s.withDevice(body)
	}
	status, header, resp, err := s.forward(r.Context(), r.Method, path, r.URL.RawQuery, r.Header, body)
	if err != nil && path == "/auth/logout" && r.Method == "POST" {
		// Logging out works offline; the server session expires on its own (§7.12).
		_ = s.dropSession(false)
		w.WriteHeader(204)
		return
	}
	if err != nil {
		s.writeForwardErr(w, r, path, err)
		return
	}
	s.setOnline(true)
	a := s.current()
	switch {
	case isAuth && status < 300:
		var auth api.AuthResponse
		if json.Unmarshal(resp, &auth) == nil && auth.Token != "" {
			if err := s.signedIn(auth.Account, auth.Token, auth.CleanupCredential); err != nil {
				writeErr(w, 500, "internal", "Could not save your sign-in on this device: "+err.Error())
				return
			}
			// The token and the cleanup credential stay in the shell; the webview
			// never sees them.
			resp, _ = json.Marshal(map[string]any{"account": auth.Account})
		}
	case path == "/auth/logout" && r.Method == "POST":
		_ = s.dropSession(false)
	case status == 401 && a != nil && path != "/auth/reauthenticate" && path != "/me/password":
		go s.sessionEnded(a.id)
	case a != nil && status < 300 && r.Method != "GET":
		switch {
		case path == "/me" || path == "/me/terms":
			go s.refreshMe(context.Background())
		case path == "/me/account-deletion":
			go s.purgeAccount(context.Background(), a.id)
		case path == "/me/sessions/revoke-all":
			go s.dropSession(false)
		}
		a.reconcileSoon()
		resp = a.mergeOverlay(r.Context(), resp)
	}
	if ct := header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

// withDevice adds this installation to login and registration (§6.2).
func (s *Shell) withDevice(body []byte) []byte {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	s.mu.Lock()
	m["device"] = map[string]string{"installation_id": s.st.InstallationID, "label": s.cfg.DeviceLabel}
	s.mu.Unlock()
	out, _ := json.Marshal(m)
	return out
}

var proxyClient = &http.Client{Timeout: 60 * time.Second}

type sentError struct{ err error }

func (e sentError) Error() string { return e.err.Error() }

func (s *Shell) forward(ctx context.Context, method, path, query string, in http.Header, body []byte) (int, http.Header, []byte, error) {
	u := s.cfg.APIBase + path
	if query != "" {
		u += "?" + query
	}
	var rd io.Reader
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, nil, err
	}
	for _, h := range []string{"Content-Type", "Idempotency-Key", "X-Request-ID", "Accept-Language"} {
		if v := in.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	req.Header.Set("X-Client-Platform", "desktop")
	req.Header.Set("X-Client-Version", s.cfg.Version)
	if t := s.client.Token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	res, err := proxyClient.Do(req)
	if err != nil {
		var ue *url.Error
		var op *net.OpError
		if errors.As(err, &ue) && errors.As(ue.Err, &op) && op.Op == "dial" {
			return 0, nil, nil, api.ErrConnection
		}
		return 0, nil, nil, sentError{err}
	}
	defer res.Body.Close()
	resp, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, nil, sentError{err}
	}
	return res.StatusCode, res.Header, resp, nil
}

func (s *Shell) writeForwardErr(w http.ResponseWriter, r *http.Request, path string, err error) {
	a := s.current()
	if errors.Is(err, api.ErrConnection) {
		s.setOnline(false)
		// Offline start: the account and its library are local (R-PLAY-1).
		if r.Method == "GET" && path == "/me" && a != nil {
			writeJSON(w, 200, a.Me())
			return
		}
		w.Header().Set(HeaderOffline, "1")
		writeErr(w, 503, "connection_required", "Connect to the server to make changes. This change was not saved.")
		return
	}
	w.Header().Set(HeaderUnknown, "1")
	writeErr(w, 504, "internal", "Could not confirm this change. Reconnect to check its status.")
}

// mergeOverlay adds this device's overlay entries to a playlist the server
// returned, so the UI keeps showing local songs in it.
func (a *account) mergeOverlay(ctx context.Context, resp []byte) []byte {
	if !bytes.Contains(resp, []byte(`"playlist"`)) {
		return resp
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(resp, &m) != nil || m["playlist"] == nil {
		return resp
	}
	var p api.Playlist
	if json.Unmarshal(m["playlist"], &p) != nil || p.ID == "" {
		return resp
	}
	merged, err := a.mergedPlaylist(ctx, p)
	if err != nil {
		return resp
	}
	// The replica may not have this revision yet: merge the server's items with
	// the overlay entries directly.
	items := p.Items
	for _, it := range merged.Items {
		if isLocal(it.TrackID) {
			items = append(items, it)
		}
	}
	sortPlaylistItems(items)
	p.Items = items
	m["playlist"], _ = json.Marshal(p)
	out, _ := json.Marshal(m)
	return out
}
