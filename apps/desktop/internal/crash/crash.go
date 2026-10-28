// Package crash reports the desktop app's crashes to Sentry (D-35, §4.4). Reports
// are on by default with an opt-out in Settings, and every event is rebuilt from
// an allowlist and scrubbed first: no song, artist, playlist or group names, file
// paths, emails, tokens, URLs, user, device name or breadcrumbs ever leave the
// machine. Without a DSN (development, tests) nothing is sent.
package crash

import (
	"bytes"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/henriquepaulalima/song-sleeve/server/pkg/scrub"
)

type Config struct {
	DSN         string
	Release     string
	Environment string
	// Enabled is the Settings switch, read for every event.
	Enabled func() bool
}

// tunnelClient forwards the page's reports; Sentry answering slowly mustn't keep
// requests open.
var tunnelClient = &http.Client{Timeout: 10 * time.Second}

var (
	cfg      Config
	envelope string            // the DSN's envelope endpoint, for the webview's reports
	auth     map[string]string // its auth headers
)

// Init starts reporting; it does nothing without a DSN.
func Init(c Config) error {
	cfg = c
	if c.DSN == "" {
		return nil
	}
	dsn, err := sentry.NewDsn(c.DSN)
	if err != nil {
		return err
	}
	envelope, auth = dsn.GetAPIURL().String(), dsn.RequestHeaders()
	return sentry.Init(sentry.ClientOptions{
		Dsn:            c.DSN,
		Release:        c.Release,
		Environment:    c.Environment,
		MaxBreadcrumbs: -1,
		ServerName:     "-", // never the computer's name
		SendDefaultPII: false,
		BeforeSend: func(e *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			if !enabled() {
				return nil
			}
			return Scrub(e)
		},
	})
}

func enabled() bool { return cfg.DSN != "" && (cfg.Enabled == nil || cfg.Enabled()) }

// Recover reports a panic and lets it continue (the process still crashes).
// Use as `defer crash.Recover()` at the top of goroutines.
func Recover() {
	if r := recover(); r != nil {
		if enabled() {
			sentry.CurrentHub().Recover(r)
			sentry.Flush(3 * time.Second)
		}
		panic(r)
	}
}

// Scrub rebuilds an event from the parts that explain a crash.
func Scrub(e *sentry.Event) *sentry.Event {
	out := &sentry.Event{
		EventID: e.EventID, Timestamp: e.Timestamp, Level: e.Level, Platform: e.Platform,
		Release: e.Release, Environment: e.Environment, Sdk: e.Sdk, Type: e.Type,
		Message: scrub.Text(e.Message), ServerName: "-",
		Contexts: map[string]sentry.Context{},
	}
	for _, k := range []string{"os", "runtime"} {
		if c, ok := e.Contexts[k]; ok {
			out.Contexts[k] = c
		}
	}
	for _, x := range e.Exception {
		x.Value = scrub.Text(x.Value)
		x.Stacktrace = frames(x.Stacktrace)
		out.Exception = append(out.Exception, x)
	}
	for _, t := range e.Threads {
		t.Name = ""
		t.Stacktrace = frames(t.Stacktrace)
		out.Threads = append(out.Threads, t)
	}
	return out
}

// frames keeps function, package, file name and line; no paths, source or
// variables.
func frames(st *sentry.Stacktrace) *sentry.Stacktrace {
	if st == nil {
		return nil
	}
	out := &sentry.Stacktrace{}
	for _, f := range st.Frames {
		out.Frames = append(out.Frames, sentry.Frame{
			Function: f.Function, Module: f.Module, Filename: path.Base(strings.ReplaceAll(f.Filename, `\`, "/")),
			Lineno: f.Lineno, Colno: f.Colno, InApp: f.InApp,
		})
	}
	return out
}

// Tunnel forwards the webview's reports (Sentry's tunnel option): they are
// scrubbed in the page, and dropped here when reports are off. They only ever go
// to this build's own DSN.
func Tunnel(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(400)
		return
	}
	if !enabled() || envelope == "" {
		w.WriteHeader(204)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), "POST", envelope, bytes.NewReader(body))
	if err != nil {
		w.WriteHeader(500)
		return
	}
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	for k, v := range auth {
		req.Header.Set(k, v)
	}
	res, err := tunnelClient.Do(req)
	if err != nil {
		w.WriteHeader(502)
		return
	}
	res.Body.Close()
	w.WriteHeader(res.StatusCode)
}
