// Package config reads the server settings from the environment. Every timing and
// limit that the plan calls a "server setting" lives here so it can be tuned without
// design changes (R-SYNC-5, R-LIM-5, F-5).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env         string // "development", "staging" or "production"
	Port        string
	DatabaseURL string

	// Public base URL of the web app, used in emailed links (reset page, D-27).
	WebBaseURL string
	// Exact origins allowed for credentialed CORS (R-AUTH-3).
	AllowedOrigins []string
	// CIDRs of proxies whose X-Forwarded-For is trusted for the client IP used by
	// rate limits (the Railway edge and the Vercel /api rewrite in production).
	TrustedProxies []string
	// Cookies are Secure everywhere except plain-HTTP local development.
	CookieSecure bool

	// Secrets. SessionSecret signs CSRF tokens; DataKey encrypts transient
	// email link material at rest (§5.1 email_jobs). Both are 32+ byte strings.
	SessionSecret string
	DataKey       string

	CloudSyncAllowlist []string // normalized emails (R-CLOUD-8)
	TermsVersion       string   // R-AUTH-10

	MinClientVersion map[string]string // per platform (F-5)

	S3Endpoint        string
	S3PublicEndpoint  string // endpoint browsers use for presigned URLs; defaults to S3Endpoint
	S3Bucket          string
	S3Region          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3PathStyle       bool
	S3EnsureBucket    bool // development only: create the bucket and its CORS rule

	EmailFrom    string
	SMTPAddr     string // development: Mailpit
	ResendAPIKey string // staging/production

	FFprobePath string

	SessionLifetime      time.Duration
	HeartbeatInterval    time.Duration
	StallTimeout         time.Duration
	UploadGrantTTL       time.Duration
	DownloadGrantTTL     time.Duration
	ReceiptRetention     time.Duration
	EventRetention       time.Duration
	ResetTokenTTL        time.Duration
	ReauthProofTTL       time.Duration
	UploadPartSize       int64
	DefaultQuotaBytes    int64
	ProgressHintInterval time.Duration
}

func Load() (*Config, error) {
	c := &Config{
		Env:                  env("APP_ENV", "development"),
		Port:                 env("PORT", "8088"),
		DatabaseURL:          env("DATABASE_URL", "postgres://songsleeve:songsleeve@localhost:55432/songsleeve?sslmode=disable"),
		WebBaseURL:           strings.TrimRight(env("WEB_BASE_URL", "http://localhost:5173"), "/"),
		AllowedOrigins:       list(env("ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")),
		TrustedProxies:       list(env("TRUSTED_PROXIES", "")),
		SessionSecret:        env("SESSION_SECRET", ""),
		DataKey:              env("DATA_KEY", ""),
		CloudSyncAllowlist:   lowerList(env("CLOUD_SYNC_ALLOWLIST", "")),
		TermsVersion:         env("TERMS_VERSION", "2026-10-draft"),
		S3Endpoint:           env("S3_ENDPOINT", "http://localhost:58333"),
		S3PublicEndpoint:     env("S3_PUBLIC_ENDPOINT", ""),
		S3Bucket:             env("S3_BUCKET", "song-sleeve"),
		S3Region:             env("S3_REGION", "auto"),
		S3AccessKeyID:        env("S3_ACCESS_KEY_ID", "songsleeve"),
		S3SecretAccessKey:    env("S3_SECRET_ACCESS_KEY", "songsleeve-secret"),
		EmailFrom:            env("EMAIL_FROM", "Song Sleeve <no-reply@songsleeve.local>"),
		SMTPAddr:             env("SMTP_ADDR", ""),
		ResendAPIKey:         env("RESEND_API_KEY", ""),
		FFprobePath:          env("FFPROBE_PATH", "ffprobe"),
		SessionLifetime:      90 * 24 * time.Hour,
		HeartbeatInterval:    dur("HEARTBEAT_INTERVAL", 30*time.Second),
		StallTimeout:         dur("STALL_TIMEOUT", 5*time.Minute),
		UploadGrantTTL:       dur("UPLOAD_GRANT_TTL", 5*time.Minute),
		DownloadGrantTTL:     dur("DOWNLOAD_GRANT_TTL", 15*time.Minute),
		ReceiptRetention:     dur("RECEIPT_RETENTION", 90*24*time.Hour),
		EventRetention:       dur("EVENT_RETENTION", 90*24*time.Hour),
		ResetTokenTTL:        30 * time.Minute,
		ReauthProofTTL:       5 * time.Minute,
		UploadPartSize:       int64(num("UPLOAD_PART_SIZE", 8<<20)),
		DefaultQuotaBytes:    int64(num("DEFAULT_QUOTA_BYTES", 26843545600)),
		ProgressHintInterval: dur("PROGRESS_HINT_INTERVAL", 3*time.Second),
		MinClientVersion: map[string]string{
			"web":     env("MIN_CLIENT_VERSION_WEB", "0.1.0"),
			"desktop": env("MIN_CLIENT_VERSION_DESKTOP", "0.1.0"),
			"android": env("MIN_CLIENT_VERSION_ANDROID", "0.1.0"),
		},
	}
	if c.S3PublicEndpoint == "" {
		c.S3PublicEndpoint = c.S3Endpoint
	}
	dev := c.Env == "development"
	c.CookieSecure = !dev
	c.S3PathStyle = boolean("S3_PATH_STYLE", dev)
	c.S3EnsureBucket = boolean("S3_ENSURE_BUCKET", dev)
	if dev && c.SMTPAddr == "" && c.ResendAPIKey == "" {
		c.SMTPAddr = "localhost:51025"
	}
	if c.SessionSecret == "" || c.DataKey == "" {
		if !dev {
			return nil, fmt.Errorf("SESSION_SECRET and DATA_KEY are required outside development")
		}
		// Fixed development secrets so sessions survive restarts locally.
		if c.SessionSecret == "" {
			c.SessionSecret = "dev-session-secret-not-for-production-use"
		}
		if c.DataKey == "" {
			c.DataKey = "dev-data-key-not-for-production-use-000"
		}
	}
	return c, nil
}

func (c *Config) IsDevelopment() bool { return c.Env == "development" }

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

func list(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func lowerList(s string) []string {
	out := list(s)
	for i := range out {
		out[i] = strings.ToLower(out[i])
	}
	return out
}

func dur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func num(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func boolean(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}
