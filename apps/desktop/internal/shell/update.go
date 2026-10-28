package shell

import (
	"context"
	"net/http"
	"net/url"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/update"
)

type updateDoc struct {
	Enabled bool `json:"enabled"`
	update.Status
}

// serveUpdate answers Settings' update card and the update-required screen
// (§8.3, §4.4). It works signed out: an old app may be refused at sign-in.
func (s *Shell) serveUpdate(w http.ResponseWriter, r *http.Request, path string) {
	u := s.cfg.Updater
	switch {
	case path == "/update" && r.Method == "GET":
		if u == nil {
			writeJSON(w, 200, updateDoc{Status: update.Status{Current: s.cfg.Version, State: "idle"}})
			return
		}
		writeJSON(w, 200, updateDoc{Enabled: u.Enabled(), Status: u.Status()})

	case path == "/update/check" && r.Method == "POST":
		if u == nil || !u.Enabled() {
			writeErr(w, 409, "update_unavailable", "This build doesn't update itself.")
			return
		}
		// A download can take minutes: progress arrives as "update" events.
		go u.Check(context.WithoutCancel(r.Context()))
		w.WriteHeader(202)

	case path == "/update/restart" && r.Method == "POST":
		if u == nil || u.Status().State != "ready" || s.cfg.Hooks.Restart == nil {
			writeErr(w, 409, "update_not_ready", "No update is ready.")
			return
		}
		if err := s.cfg.Hooks.Restart(); err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
		w.WriteHeader(204)

	case path == "/open-url" && r.Method == "POST":
		var req struct {
			URL string `json:"url"`
		}
		if err := decode(r, &req); err != nil {
			writeErr(w, 400, "invalid_request", "Invalid body.")
			return
		}
		if p, err := url.Parse(req.URL); err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" {
			writeErr(w, 400, "invalid_request", "Only web links open in the browser.")
			return
		}
		if s.cfg.Hooks.Open == nil {
			writeErr(w, 400, "invalid_request", "No browser.")
			return
		}
		if err := s.cfg.Hooks.Open(req.URL); err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
		w.WriteHeader(204)

	default:
		writeErr(w, 404, "not_found", "Not found.")
	}
}
