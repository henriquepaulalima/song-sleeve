//go:build integration

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/henriquepaulalima/song-sleeve/server/internal/position"
)

const testPassword = "saltwater-hymn"

// resetRateLimits keeps tests that register many accounts under the per-IP limit.
func resetRateLimits(t *testing.T) {
	t.Helper()
	if _, err := deps.Pool.Exec(context.Background(), `DELETE FROM rate_limit_buckets`); err != nil {
		t.Fatal(err)
	}
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%s@example.com", prefix, uuid.NewString()[:8])
}

// newAppAccount registers an installed-app (bearer) account, which needs no
// allowlist. It returns the client and the account document.
func newAppAccount(t *testing.T, email string) (*client, map[string]any) {
	t.Helper()
	resetRateLimits(t)
	c := newClient(t, "desktop")
	body := expect(t, c.do("POST", "/auth/register", map[string]any{
		"email": email, "password": testPassword, "accepted_terms": true, "terms_version": "2026-10-draft",
		"device": map[string]any{"installation_id": uuid.NewString(), "label": "Test PC"},
	}, ""), http.StatusCreated)
	c.token = body["token"].(string)
	return c, body["account"].(map[string]any)
}

// loginApp signs an existing account in from another installed device.
func loginApp(t *testing.T, email, password string) *client {
	t.Helper()
	c := newClient(t, "desktop")
	body := expect(t, c.do("POST", "/auth/login", map[string]any{
		"email": email, "password": password, "device": map[string]any{"installation_id": uuid.NewString(), "label": "Other"},
	}, ""), http.StatusOK)
	c.token = body["token"].(string)
	return c
}

// newWebSyncAccount registers one of the extra allowlisted emails on the web.
func newWebSyncAccount(t *testing.T, i int) *client {
	t.Helper()
	resetRateLimits(t)
	return signInFresh(t, syncEmails[i])
}

func userID(t *testing.T, c *client) string {
	t.Helper()
	return expect(t, c.do("GET", "/me", nil, ""), 200)["id"].(string)
}

// insertTracks writes ready tracks straight into the catalog for list and order
// tests that don't need audio. It returns their IDs in library order.
func insertTracks(t *testing.T, uid string, n int) []string {
	t.Helper()
	ctx := context.Background()
	var last *string
	if err := deps.Pool.QueryRow(ctx, `SELECT max(library_position COLLATE "C") FROM tracks WHERE user_id = $1`, uid).Scan(&last); err != nil {
		t.Fatal(err)
	}
	pos := ""
	if last != nil {
		pos = *last
	}
	ids := make([]string, n)
	for i := range n {
		pos = position.After(pos)
		id, _ := uuid.NewV7()
		ids[i] = id.String()
		if _, err := deps.Pool.Exec(ctx, `
			INSERT INTO tracks (id, user_id, name, duration_ms, byte_size, format, sha256, state, library_position)
			VALUES ($1, $2, $3, 1000, 100, 'mp3', $4, 'ready', $5)`,
			id, uid, fmt.Sprintf("Track %04d", i), strings.Repeat("c", 64), pos); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func ids(list []any, key string) []string {
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = x.(map[string]any)[key].(string)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

const mailpit = "http://localhost:58025"

type mail struct {
	ID      string `json:"ID"`
	Subject string `json:"Subject"`
	Created string `json:"Created"`
}

// waitForMail returns the text of the newest email to addr whose subject contains
// subject, sent after since.
func waitForMail(t *testing.T, addr, subject string, since time.Time) string {
	t.Helper()
	end := time.Now().Add(20 * time.Second)
	for time.Now().Before(end) {
		res, err := http.Get(mailpit + "/api/v1/search?query=" + url.QueryEscape("to:"+addr))
		if err == nil {
			var list struct {
				Messages []mail `json:"messages"`
			}
			_ = json.NewDecoder(res.Body).Decode(&list)
			res.Body.Close()
			for _, m := range list.Messages {
				created, _ := time.Parse(time.RFC3339Nano, m.Created)
				if strings.Contains(m.Subject, subject) && created.After(since.Add(-2*time.Second)) {
					r, err := http.Get(mailpit + "/api/v1/message/" + m.ID)
					if err != nil {
						t.Fatal(err)
					}
					var full struct {
						Text string `json:"Text"`
					}
					_ = json.NewDecoder(r.Body).Decode(&full)
					r.Body.Close()
					return full.Text
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("no %q email to %s", subject, addr)
	return ""
}

var tokenRe = regexp.MustCompile(`reset#token=([\w-]+)`)

// sseEvents opens the event stream and returns a channel of event names.
func sseEvents(t *testing.T, c *client, ctx context.Context) <-chan string {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/me/events", nil)
	req.Header.Set("X-Client-Platform", c.platform)
	req.Header.Set("X-Client-Version", "0.1.0")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	stream := &http.Client{Jar: c.http.Jar}
	res, err := stream.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("event stream: %d", res.StatusCode)
	}
	out := make(chan string, 16)
	go func() {
		defer res.Body.Close()
		defer close(out)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				out <- name
			}
		}
	}()
	return out
}

func nextEvent(t *testing.T, ch <-chan string, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed before %q", want)
			}
			if e == want {
				return
			}
		case <-deadline:
			t.Fatalf("no %q event within %s", want, timeout)
		}
	}
}
