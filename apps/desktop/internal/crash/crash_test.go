package crash

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

// sentryServer stands in for Sentry's envelope endpoint.
type sentryServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
	auth   []string
}

func newSentry(t *testing.T) *sentryServer {
	s := &sentryServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(b))
		s.auth = append(s.auth, r.Header.Get("X-Sentry-Auth"))
		s.mu.Unlock()
		w.WriteHeader(200)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *sentryServer) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func TestReportsAreScrubbedAndOptional(t *testing.T) {
	srv := newSentry(t)
	var on atomic.Bool
	on.Store(true)
	dsn := strings.Replace(srv.URL, "http://", "http://publickey@", 1) + "/42"
	if err := Init(Config{DSN: dsn, Release: "0.1.0", Environment: "test", Enabled: on.Load}); err != nil {
		t.Fatal(err)
	}
	sentry.CurrentHub().Scope().SetUser(sentry.User{Email: "alex@example.com"})
	sentry.CurrentHub().Scope().SetTag("song", "Low Tide")

	sentry.CaptureException(errors.New(`open /home/alex/Music/Low Tide.flac: "Low Tide" by alex@example.com`))
	sentry.Flush(5 * time.Second)
	got := srv.received()
	if len(got) != 1 {
		t.Fatalf("events sent: %d", len(got))
	}
	for _, leak := range []string{"Low Tide", "/home/alex", "alex@example.com"} {
		if strings.Contains(got[0], leak) {
			t.Errorf("the report contains %q:\n%s", leak, got[0])
		}
	}
	if !strings.Contains(got[0], "open [path]") {
		t.Errorf("the error itself is missing:\n%s", got[0])
	}

	// Switched off in Settings: nothing goes out.
	on.Store(false)
	sentry.CaptureException(errors.New("after opting out"))
	sentry.Flush(2 * time.Second)
	if n := len(srv.received()); n != 1 {
		t.Fatalf("a report was sent with crash reports off (%d)", n)
	}

	// The webview's reports go through the tunnel, only while reports are on.
	tunnel := func() int {
		rec := httptest.NewRecorder()
		Tunnel(rec, httptest.NewRequest("POST", "/local/sentry", strings.NewReader("{}\n{\"type\":\"event\"}\n{}")))
		return rec.Code
	}
	if code := tunnel(); code != 204 || len(srv.received()) != 1 {
		t.Fatalf("tunnel while off: %d, %d sent", code, len(srv.received()))
	}
	on.Store(true)
	if code := tunnel(); code != 200 || len(srv.received()) != 2 {
		t.Fatalf("tunnel while on: %d, %d sent", code, len(srv.received()))
	}
	// Sentry hanging doesn't keep the page's request open.
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(2 * time.Second) }))
	defer hang.Close()
	saved, savedClient := envelope, tunnelClient
	envelope, tunnelClient = hang.URL, &http.Client{Timeout: 200 * time.Millisecond}
	start := time.Now()
	if code := tunnel(); code != 502 || time.Since(start) > time.Second {
		t.Fatalf("tunnel to a hanging Sentry: %d after %s", code, time.Since(start))
	}
	envelope, tunnelClient = saved, savedClient

	srv.mu.Lock()
	auth := srv.auth[1]
	srv.mu.Unlock()
	if !strings.Contains(auth, "sentry_key=publickey") {
		t.Fatalf("the tunnel didn't authenticate with this build's DSN: %q", auth)
	}
}
