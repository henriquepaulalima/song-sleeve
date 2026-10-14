package events

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
)

// SSE serves GET /me/events. It replays from the client's cursor by announcing the
// latest cursor, then streams hints. Heartbeats keep Railway's 5-minute idle limit
// away, and the stream closes itself before the 15-minute edge cap so clients
// reconnect (and the session is rechecked) on their own schedule (§10.2).
func (h *Hub) SSE(c *gin.Context) {
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	from, _ := strconv.ParseInt(c.Query("cursor"), 10, 64)

	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, unsubscribe := h.Subscribe(s.UserID)
	defer unsubscribe()

	send := func(event string, data any) bool {
		raw, _ := json.Marshal(data)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return false
		}
		w.Flush()
		return true
	}

	latest, err := LatestCursor(ctx, h.pool, s.UserID)
	if err != nil {
		return
	}
	if !send("ready", gin.H{"cursor": latest}) {
		return
	}
	if latest > from {
		send("changes", gin.H{"cursor": latest})
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	maxAge := time.NewTimer(14 * time.Minute)
	defer maxAge.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-maxAge.C:
			send("reconnect", gin.H{})
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			w.Flush()
		case n := <-ch:
			switch n.Kind {
			case "changes":
				if !send("changes", gin.H{"cursor": n.Cursor}) {
					return
				}
			case "account_deleted", "session_revoked":
				send(n.Kind, gin.H{})
				return
			default:
				if !send(n.Kind, n.Data) {
					return
				}
			}
		}
	}
}
