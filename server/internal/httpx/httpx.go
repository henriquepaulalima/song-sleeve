// Package httpx holds response helpers, middleware and the authenticated-session
// context shared by every handler package.
package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
)

// Session is the authenticated caller, set by the auth middleware.
type Session struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	DeviceID         uuid.UUID
	Transport        string // "cookie" (web) or "bearer" (installed apps)
	Email            string
	Locale           string
	CloudSyncEnabled bool
	TermsVersion     string
}

const sessionKey = "ss.session"

func SetSession(c *gin.Context, s *Session) { c.Set(sessionKey, s) }

func CurrentSession(c *gin.Context) *Session {
	v, ok := c.Get(sessionKey)
	if !ok {
		return nil
	}
	return v.(*Session)
}

// OK writes a JSON success body.
func OK(c *gin.Context, status int, body any) { c.JSON(status, body) }

// Fail writes the error envelope and aborts the chain. Unknown errors become
// "internal" and are logged without sensitive values.
func Fail(c *gin.Context, err error) {
	e, ok := apperr.As(err)
	if !ok {
		slog.ErrorContext(c.Request.Context(), "request failed", "error", err.Error(), "request_id", RequestID(c), "path", c.FullPath())
		e = apperr.New(apperr.Internal, "Something went wrong.")
	}
	if e.Code == apperr.RateLimited {
		if s, ok := e.Details["retry_after_seconds"].(int); ok {
			c.Header("Retry-After", strconv.Itoa(s))
		}
	}
	c.AbortWithStatusJSON(e.Status(), apperr.Envelope{Error: e})
}

// Bind decodes a JSON body strictly enough to reject unknown shapes.
func Bind(c *gin.Context, dst any) error {
	if err := c.ShouldBindJSON(dst); err != nil {
		return apperr.New(apperr.InvalidRequest, "Invalid request body")
	}
	return nil
}

func ParamUUID(c *gin.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, apperr.New(apperr.NotFound, "Not found")
	}
	return id, nil
}

// RequestID middleware assigns a request ID used in structured logs (§4.4).
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" || len(id) > 64 {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		c.Set("ss.request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func RequestID(c *gin.Context) string { return c.GetString("ss.request_id") }

// Logger logs one structured line per request. It never logs tokens, links,
// paths of user files or bodies.
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if c.FullPath() == "/health" {
			return
		}
		slog.Info("request",
			"request_id", RequestID(c),
			"method", c.Request.Method,
			"route", c.FullPath(),
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func Recover() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, rec any) {
		slog.Error("panic", "request_id", RequestID(c), "panic", rec)
		Fail(c, errors.New("panic"))
	})
}

// CORS allows credentialed requests only from exact origins (R-AUTH-3).
func CORS(origins []string) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowed[origin] {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Vary", "Origin")
			if c.Request.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, X-CSRF-Token, X-Client-Platform, X-Client-Version, X-Request-ID, Authorization")
				h.Set("Access-Control-Max-Age", "600")
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Next()
	}
}

// ClientVersion enforces X-Client-Platform and X-Client-Version and answers
// 426 client_update_required below the per-platform minimum (F-5).
func ClientVersion(minimums map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.GetHeader("X-Client-Platform")
		min, ok := minimums[platform]
		if !ok {
			Fail(c, apperr.New(apperr.InvalidRequest, "Missing or unknown X-Client-Platform"))
			return
		}
		if CompareVersions(c.GetHeader("X-Client-Version"), min) < 0 {
			Fail(c, apperr.WithDetails(apperr.ClientUpdateRequired, "Update Song Sleeve to keep syncing.", map[string]any{"minimum_version": min}))
			return
		}
		c.Set("ss.platform", platform)
		c.Next()
	}
}

func Platform(c *gin.Context) string { return c.GetString("ss.platform") }

// CompareVersions compares dotted numeric versions; malformed versions sort lowest.
func CompareVersions(a, b string) int {
	pa, pb := parseVersion(a), parseVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.SplitN(v, "-", 2)[0], "v")
	parts := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{-1, -1, -1}
		}
		out[i] = n
	}
	if v == "" {
		return [3]int{-1, -1, -1}
	}
	return out
}

// NoStore marks a response as never cacheable (reset page hardening, §7.12).
func NoStore() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	}
}
