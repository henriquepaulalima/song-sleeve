package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Error is an error the server answered with (§4.4 envelope).
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return fmt.Sprintf("%s (%d): %s", e.Code, e.Status, e.Message) }

// ErrConnection: the request never reached the server; nothing was changed (R-MUT-1).
var ErrConnection = errors.New("connection_required")

// UnknownResult: a change was sent but its result is unknown (R-MUT-4). Recover it
// with MutationStatus using the same request ID; never resubmit it as new.
type UnknownResult struct{ RequestID string }

func (u *UnknownResult) Error() string { return "unknown result for request " + u.RequestID }

// Code returns the server error code of err, or "".
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	if errors.Is(err, ErrConnection) {
		return "connection_required"
	}
	return ""
}

type Client struct {
	BaseURL  string // e.g. http://localhost:8088/api
	Platform string // "desktop" | "android"
	Version  string
	HTTP     *http.Client

	mu    sync.RWMutex
	token string
	// OnUnauthenticated is called on 401 so the app asks to sign in again; local
	// browsing and playback keep working (§7.12).
	OnUnauthenticated func()
	OnUpdateRequired  func()
}

func New(baseURL, platform, version string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Platform: platform, Version: version, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) SetToken(t string) {
	c.mu.Lock()
	c.token = t
	c.mu.Unlock()
}

func (c *Client) Token() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Platform", c.Platform)
	req.Header.Set("X-Client-Version", c.Version)
	if t := c.Token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	return req, nil
}

func (c *Client) decode(res *http.Response, out any) error {
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var env struct {
			Error struct {
				Code    string         `json:"code"`
				Message string         `json:"message"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		e := &Error{Status: res.StatusCode, Code: env.Error.Code, Message: env.Error.Message, Details: env.Error.Details}
		if e.Code == "" {
			e.Code = "internal"
		}
		if e.Code == "unauthenticated" && c.OnUnauthenticated != nil {
			c.OnUnauthenticated()
		}
		if e.Code == "client_update_required" && c.OnUpdateRequired != nil {
			c.OnUpdateRequired()
		}
		return e
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Do sends a read or a non-mutation POST. Network failures are ErrConnection.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrConnection
	}
	return c.decode(res, out)
}

// Mutate sends a server-data change with an Idempotency-Key (R-MUT-3). If the
// request may have reached the server but no answer came back, it returns
// UnknownResult.
func (c *Client) Mutate(ctx context.Context, method, path, requestID string, body, out any) error {
	if requestID == "" {
		requestID = uuid.NewString()
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Idempotency-Key", requestID)
	res, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ue *url.Error
		if errors.As(err, &ue) && isDialError(ue) {
			return ErrConnection // never left this machine
		}
		return &UnknownResult{RequestID: requestID}
	}
	return c.decode(res, out)
}

func isDialError(err *url.Error) bool {
	s := err.Err.Error()
	return strings.Contains(s, "connection refused") || strings.Contains(s, "no such host") || strings.Contains(s, "network is unreachable") || strings.Contains(s, "dial tcp")
}

type MutationStatus struct {
	Status      string          `json:"status"` // committed | rejected | not_applied
	HTTPStatus  int             `json:"http_status"`
	Result      json.RawMessage `json:"result"`
	OperationID *string         `json:"operation_id"`
}

func (c *Client) MutationStatus(ctx context.Context, requestID string) (*MutationStatus, error) {
	var s MutationStatus
	return &s, c.Do(ctx, "GET", "/me/mutations/"+url.PathEscape(requestID), nil, &s)
}

// Snapshot fetches every page of the catalog snapshot.
func (c *Client) Snapshot(ctx context.Context) ([]Snapshot, error) {
	var pages []Snapshot
	token := ""
	for {
		path := "/me/catalog/snapshot"
		if token != "" {
			path += "?page_token=" + url.QueryEscape(token)
		}
		var p Snapshot
		if err := c.Do(ctx, "GET", path, nil, &p); err != nil {
			return nil, err
		}
		pages = append(pages, p)
		if p.NextPageToken == "" {
			return pages, nil
		}
		token = p.NextPageToken
	}
}

func (c *Client) Changes(ctx context.Context, cursor int64) (*Changes, error) {
	var ch Changes
	return &ch, c.Do(ctx, "GET", fmt.Sprintf("/me/catalog/changes?cursor=%d", cursor), nil, &ch)
}

func (c *Client) Me(ctx context.Context) (*Me, error) {
	var m Me
	return &m, c.Do(ctx, "GET", "/me", nil, &m)
}

func (c *Client) DownloadGrant(ctx context.Context, contentID string) (*DownloadGrant, error) {
	var g DownloadGrant
	return &g, c.Do(ctx, "POST", "/me/content/"+contentID+"/download-grant", nil, &g)
}

// Event is one server-sent event.
type Event struct {
	Name string
	Data json.RawMessage
}

// Events streams the SSE hints until ctx ends or the stream closes.
func (c *Client) Events(ctx context.Context, cursor int64, fn func(Event)) error {
	req, err := c.newRequest(ctx, "GET", fmt.Sprintf("/me/events?cursor=%d", cursor), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	stream := &http.Client{} // no overall timeout on a long-lived stream
	res, err := stream.Do(req)
	if err != nil {
		return ErrConnection
	}
	if res.StatusCode != 200 {
		return c.decode(res, nil)
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var ev Event
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if ev.Name != "" {
				fn(ev)
			}
			ev = Event{}
		case strings.HasPrefix(line, "event: "):
			ev.Name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			ev.Data = json.RawMessage(strings.TrimPrefix(line, "data: "))
		}
	}
	return sc.Err()
}
