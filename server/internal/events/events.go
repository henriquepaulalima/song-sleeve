// Package events appends durable account events (R-EVT-1) and fans out hints over
// one LISTEN connection per API replica (§10.2). Hints are never data (R-EVT-2):
// clients fetch authoritative changes after a hint.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
)

const channel = "account_events"

// Entity types in the change feed.
const (
	EntityTrack       = "track"
	EntityArtist      = "artist"
	EntityPlaylist    = "playlist"
	EntityGroup       = "group"
	EntityPreferences = "preferences"
	EntityScope       = "scope"
	EntityAccount     = "account"
)

type Event struct {
	UserID     uuid.UUID
	EntityType string
	EntityID   uuid.UUID
	EventType  string
	Revision   int64
	Payload    map[string]any
}

// Append writes the event in the caller's transaction and queues a hint that
// PostgreSQL delivers only when that transaction commits.
func Append(ctx context.Context, q db.Querier, e Event) (int64, error) {
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	// Serialize a user's event appends until commit, so cursors become visible in
	// increasing order and a reader can never skip a later-committing lower cursor.
	if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(7101, hashtext($1::text))`, e.UserID); err != nil {
		return 0, err
	}
	var cursor int64
	err := q.QueryRow(ctx, `
		INSERT INTO account_events (user_id, entity_type, entity_id, event_type, revision, payload)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING cursor`,
		e.UserID, e.EntityType, e.EntityID, e.EventType, e.Revision, payload).Scan(&cursor)
	if err != nil {
		return 0, err
	}
	return cursor, notify(ctx, q, Notification{User: e.UserID, Kind: "changes", Cursor: cursor})
}

// Hint sends a coalescible, non-durable hint (sync progress, terminal account
// hints). It is not an account_events row (R-ID-4).
func Hint(ctx context.Context, q db.Querier, userID uuid.UUID, kind string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return notify(ctx, q, Notification{User: userID, Kind: kind, Data: raw})
}

func notify(ctx context.Context, q db.Querier, n Notification) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `SELECT pg_notify($1, $2)`, channel, string(raw))
	return err
}

type Notification struct {
	User   uuid.UUID       `json:"u"`
	Kind   string          `json:"k"`
	Cursor int64           `json:"c,omitempty"`
	Data   json.RawMessage `json:"d,omitempty"`
}

// Hub listens once per replica and fans notifications out to subscribers.
type Hub struct {
	pool *pgxpool.Pool
	mu   sync.Mutex
	subs map[uuid.UUID]map[chan Notification]struct{}
}

func NewHub(pool *pgxpool.Pool) *Hub {
	return &Hub{pool: pool, subs: map[uuid.UUID]map[chan Notification]struct{}{}}
}

// Run keeps a LISTEN connection open until ctx ends, reconnecting with backoff.
func (h *Hub) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := h.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("event listener disconnected", "error", err)
		h.broadcastReconnect()
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (h *Hub) listen(ctx context.Context) error {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var msg Notification
		if json.Unmarshal([]byte(n.Payload), &msg) != nil {
			continue
		}
		h.deliver(msg)
	}
}

func (h *Hub) deliver(n Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[n.User] {
		select {
		case ch <- n:
		default:
			// The subscriber is behind. Hints coalesce: dropping one is harmless
			// because the next fetch catches up from the cursor (R-EVT-3).
		}
	}
}

// broadcastReconnect tells every subscriber to catch up, since hints may have been
// missed while the listener was down.
func (h *Hub) broadcastReconnect() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for user, set := range h.subs {
		for ch := range set {
			select {
			case ch <- Notification{User: user, Kind: "changes"}:
			default:
			}
		}
	}
}

func (h *Hub) Subscribe(userID uuid.UUID) (<-chan Notification, func()) {
	ch := make(chan Notification, 32)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan Notification]struct{}{}
	}
	h.subs[userID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[userID], ch)
		if len(h.subs[userID]) == 0 {
			delete(h.subs, userID)
		}
		h.mu.Unlock()
	}
}

// LatestCursor returns the newest event cursor of a user (0 if none).
func LatestCursor(ctx context.Context, q db.Querier, userID uuid.UUID) (int64, error) {
	var c int64
	err := q.QueryRow(ctx, `SELECT COALESCE(MAX(cursor), 0) FROM account_events WHERE user_id = $1`, userID).Scan(&c)
	return c, err
}
